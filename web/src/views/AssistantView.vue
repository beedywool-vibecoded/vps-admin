<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import M3Switch from '@/components/M3Switch.vue'
import M3Dialog from '@/components/M3Dialog.vue'
import M3Select from '@/components/M3Select.vue'
import M3Expand from '@/components/M3Expand.vue'
import {
  api,
  streamConversation,
  type AssistantProvider,
  type AssistantSettings,
  type Block,
  type ConvMessage,
  type Conversation,
  type ToolCall,
} from '@/lib/api'
import { markdown } from '@/lib/markdown'
import { session } from '@/lib/session'
import { ago } from '@/lib/format'
import { confirm, toast, toastError } from '@/lib/ui'

const props = defineProps<{ id?: string }>()
const router = useRouter()

const settings = ref<AssistantSettings | null>(null)
const models = ref<{ id: string; label: string; hint?: string }[]>([])
const conversations = ref<Conversation[]>([])
const conv = ref<Conversation | null>(null)
const messages = ref<ConvMessage[]>([])
const calls = ref<Record<string, ToolCall>>({})
const draft = ref<Record<number, { kind: 'text' | 'thinking'; text: string }>>({})
const input = ref('')
const sending = ref(false)
const keyOpen = ref(false)
const newKey = ref('')
const savingKey = ref(false)
const urlOpen = ref(false)
const newURL = ref('')
const savingURL = ref(false)
const scroller = ref<HTMLElement>()
let close: (() => void) | undefined

const NEVER = ['Delete Docker volumes', 'Change SSH or the firewall', 'Read keys and secrets', 'Power off the server']
const busy = computed(() => conv.value?.state === 'running')
const providers = computed<AssistantProvider[]>(() => settings.value?.providers ?? [])
const active = computed(() => providers.value.find((p) => p.id === settings.value?.provider) ?? null)
const label = computed(() => active.value?.label ?? 'The assistant')
// The sidebar keeps its provider state folded away, so the chat says out loud why
// nothing can answer.
const banner = computed(() =>
  active.value?.needsKey
    ? `${label.value} is not connected, so the assistant cannot answer yet.`
    : `${label.value} is not answering at ${active.value?.url ?? 'its address'}.`,
)
const keyTitle = computed(() =>
  active.value?.id === 'claude-code' ? 'Claude subscription token' : 'Gemini API key',
)

interface Item {
  key: string
  kind: 'user' | 'text' | 'thinking' | 'tools'
  text?: string
  tools?: ToolCall[]
}

// Turns the stored API messages into what the chat shows. Tool results are not shown
// as messages: they live on the tool cards.
const items = computed<Item[]>(() => {
  const out: Item[] = []
  for (const m of messages.value) {
    const content = m.content.content
    const blocks: Block[] = typeof content === 'string' ? [{ type: 'text', text: content }] : content
    if (m.role === 'user') {
      const text = blocks.filter((b): b is { type: 'text'; text: string } => b.type === 'text').map((b) => b.text).join('\n')
      if (text) out.push({ key: `u${m.seq}`, kind: 'user', text })
      continue
    }
    let tools: ToolCall[] = []
    const flush = (i: number) => {
      if (tools.length) out.push({ key: `t${m.seq}-${i}`, kind: 'tools', tools })
      tools = []
    }
    blocks.forEach((b, i) => {
      if (b.type === 'tool_use') {
        const tc = calls.value[(b as { id: string }).id]
        if (tc) tools.push(tc)
        return
      }
      flush(i)
      if (b.type === 'text' && (b as { text: string }).text) out.push({ key: `a${m.seq}-${i}`, kind: 'text', text: (b as { text: string }).text })
      if (b.type === 'thinking' && (b as { thinking: string }).thinking) out.push({ key: `h${m.seq}-${i}`, kind: 'thinking', text: (b as { thinking: string }).thinking })
    })
    flush(blocks.length)
  }
  for (const [i, d] of Object.entries(draft.value)) out.push({ key: `d${i}`, kind: d.kind, text: d.text })
  // Calls not yet attached to a stored message (streamed before it is saved)
  const shown = new Set(out.flatMap((it) => it.tools?.map((t) => t.id) ?? []))
  const loose = Object.values(calls.value).filter((t) => !shown.has(t.id))
  if (loose.length) out.push({ key: 'loose', kind: 'tools', tools: loose })
  return out
})

async function loadSettings() {
  const res = await api.assistant()
  settings.value = res.settings
  models.value = res.models
}
async function loadList() {
  conversations.value = (await api.conversations()).conversations
}

async function open(id?: string) {
  close?.()
  draft.value = {}
  if (!id) {
    conv.value = null
    messages.value = []
    calls.value = {}
    return
  }
  try {
    const res = await api.conversation(id)
    conv.value = res.conversation
    messages.value = res.messages
    calls.value = Object.fromEntries(res.toolCalls.map((t) => [t.id, t]))
    scrollDown(true)
  } catch (e) {
    toastError(e)
    return router.replace('/assistant')
  }
  close = streamConversation(id, {
    message: (m) => {
      if (!messages.value.some((x) => x.seq === m.seq)) messages.value.push(m)
      if (m.role === 'assistant') draft.value = {}
      scrollDown()
    },
    text: (d) => append(d.index, 'text', d.text),
    thinking: (d) => append(d.index, 'thinking', d.text),
    tool: (t) => {
      calls.value = { ...calls.value, [t.id]: t }
      scrollDown()
    },
    state: (s) => {
      if (conv.value) conv.value = { ...conv.value, state: s.state, error: s.error }
      if (s.state !== 'running') loadList()
    },
  })
}

function append(index: number, kind: 'text' | 'thinking', text: string) {
  const cur = draft.value[index]
  draft.value = { ...draft.value, [index]: { kind, text: (cur?.text ?? '') + text } }
  scrollDown()
}

async function scrollDown(force = false) {
  const el = scroller.value
  const near = !el || el.scrollHeight - el.scrollTop - el.clientHeight < 200
  await nextTick()
  if ((force || near) && scroller.value) scroller.value.scrollTop = scroller.value.scrollHeight
}

async function send() {
  const text = input.value.trim()
  if (!text || sending.value) return
  sending.value = true
  try {
    if (!conv.value) {
      const { conversation } = await api.newConversation(text)
      input.value = ''
      await loadList()
      await router.push(`/assistant/${conversation.id}`)
    } else {
      await api.sendMessage(conv.value.id, text)
      input.value = ''
    }
  } catch (e) {
    toastError(e)
  } finally {
    sending.value = false
  }
}

function onKey(e: KeyboardEvent) {
  if (e.key === 'Enter' && !e.shiftKey && !e.isComposing) {
    e.preventDefault()
    send()
  }
}

async function decide(t: ToolCall, approve: boolean) {
  try {
    const res = await api.decide(t.id, approve)
    calls.value = { ...calls.value, [res.id]: res }
  } catch (e) {
    toastError(e)
  }
}

async function choose(p: AssistantProvider) {
  if (!p.active) {
    try {
      settings.value = (await api.assistantUpdate({ provider: p.id })).settings
    } catch (e) {
      toastError(e)
      return
    }
  }
  if (p.needsKey && !p.ready) keyOpen.value = true
}

async function saveKey() {
  savingKey.value = true
  try {
    const key = newKey.value.trim()
    const res = await api.assistantUpdate({ apiKey: key })
    settings.value = res.settings
    newKey.value = ''
    keyOpen.value = false
    // A stored key is not a working provider. The server says why it is not ready (no
    // claude command on this server, a check that did not answer), and that reason is the
    // answer; "API key removed" is only true when the field was left empty.
    if (key === '') toast('API key removed')
    else if (res.settings.configured) toast(`${label.value} connected`)
    else toast(res.settings.hint || `${label.value} saved, but it cannot answer yet`, true)
  } catch (e) {
    toastError(e)
  } finally {
    savingKey.value = false
  }
}

async function saveURL() {
  savingURL.value = true
  try {
    settings.value = (await api.assistantUpdate({ ollamaUrl: newURL.value.trim() })).settings
    urlOpen.value = false
    toast('Ollama address saved')
  } catch (e) {
    toastError(e)
  } finally {
    savingURL.value = false
  }
}

async function setOption(o: { model?: string; effort?: string; autoRead?: boolean }) {
  try {
    settings.value = (await api.assistantUpdate(o)).settings
  } catch (e) {
    toastError(e)
  }
}
const model = computed({
  get: () => settings.value?.model ?? '',
  set: (v) => setOption({ model: v }),
})
const effort = computed({
  get: () => settings.value?.effort ?? 'high',
  set: (v) => setOption({ effort: v }),
})
// A provider with nothing to list, or an agent that answers without the field, must leave
// the menu empty rather than throw: a render error took the whole settings card with it.
const modelOptions = computed(() =>
  (active.value?.models ?? models.value ?? []).map((m) => ({ value: m.id, label: m.label, hint: m.hint })),
)
const EFFORTS = [
  { value: 'low', label: 'Low', hint: 'Quick answers, lowest cost' },
  { value: 'medium', label: 'Medium', hint: 'Everyday questions' },
  { value: 'high', label: 'High', hint: 'Careful investigation' },
  { value: 'xhigh', label: 'Extra high', hint: 'Hard incidents' },
  { value: 'max', label: 'Max', hint: 'When correctness matters most' },
]
const autoRead = computed({
  get: () => settings.value?.autoRead ?? true,
  set: (v) => setOption({ autoRead: v }),
})

async function remove(c: Conversation) {
  const ok = await confirm({ title: 'Delete this conversation?', body: c.title, action: 'Delete', danger: true })
  if (!ok) return
  try {
    await api.deleteConversation(c.id)
    await loadList()
    if (conv.value?.id === c.id) router.replace('/assistant')
  } catch (e) {
    toastError(e)
  }
}

const SUGGESTIONS = [
  'Is anything wrong on this server right now?',
  'Why is the disk filling up?',
  'Check the security report and tell me what to fix first',
]

const statusLabel = (t: ToolCall) =>
  ({ pending: 'needs your approval', running: 'running…', done: t.isError ? 'failed' : 'done', denied: 'denied', blocked: 'blocked' })[t.status]

watch(() => props.id, (id) => open(id))
onMounted(async () => {
  document.documentElement.classList.add('assistant-page')
  try {
    await Promise.all([loadSettings(), loadList()])
  } catch (e) {
    toastError(e)
  }
  open(props.id)
})
onBeforeUnmount(() => {
  close?.()
  document.documentElement.classList.remove('assistant-page')
})
</script>

<template>
  <div class="assistant">
    <aside class="side" aria-label="Assistant">
      <div class="hist-head">
        <h1>Conversations</h1>
        <RouterLink to="/assistant" class="btn btn-tonal st"><Icon name="plus" :size="18" /> New</RouterLink>
      </div>

      <nav class="conv-list" aria-label="Conversations">
        <p v-if="!conversations.length" class="small muted">None yet. Ask something to start one.</p>
        <div v-for="c in conversations" :key="c.id" class="conv" :class="{ sel: c.id === conv?.id }">
          <RouterLink :to="`/assistant/${c.id}`" class="conv-link st">
            <span class="ellipsis">{{ c.title }}</span>
            <span class="small muted">{{ c.state === 'awaiting' ? 'waiting for you' : ago(c.updatedAt) }}</span>
          </RouterLink>
          <button class="icon-btn st" type="button" :aria-label="`Delete ${c.title}`" @click="remove(c)"><Icon name="trash" :size="18" /></button>
        </div>
      </nav>

      <section class="panel settings" aria-label="Assistant settings">
        <M3Expand>
          <template #summary>
            <span class="dot" :class="{ on: active?.ready }" aria-hidden="true" />
            <span class="strong">Assistant settings</span>
            <span class="small muted ellipsis sum">{{ label }}</span>
          </template>
          <div class="settings-body">
            <h2 id="acct">AI provider</h2>
            <div class="providers" role="radiogroup" aria-label="AI provider">
              <button
                v-for="p in providers"
                :key="p.id"
                type="button"
                class="prov st"
                role="radio"
                :aria-checked="p.active"
                :class="{ sel: p.active }"
                @click="choose(p)"
              >
                <span class="dot" :class="{ on: p.ready }" aria-hidden="true" />
                <span class="grow ptext">
                  <span class="strong">{{ p.label }}</span>
                  <span class="small muted">{{ p.badge }}</span>
                </span>
                <Icon v-if="p.active" name="check" :size="18" />
              </button>
            </div>

            <p v-if="active?.hint" class="small muted">{{ active.hint }}</p>

            <!-- Only once the provider answers: before that the list is a stand-in, and
                 choosing a model for something that cannot run reads as noise. -->
            <M3Select v-if="modelOptions.length && active?.ready" v-model="model" label="Model" :options="modelOptions" :disabled="!settings" />

            <div v-if="active?.needsKey" class="key">
              <span class="dot" :class="{ on: active.ready }" aria-hidden="true" />
              <div class="grow">
                <div class="strong">{{ active.ready ? 'Connected' : 'Not connected' }}</div>
                <code v-if="active.keyHint" class="small muted">{{ active.keyHint }}</code>
              </div>
              <button class="btn btn-text st" type="button" @click="keyOpen = true">{{ active.ready ? 'Change' : 'Connect' }}</button>
            </div>

            <template v-else-if="active">
              <div class="key">
                <span class="dot" :class="{ on: active.ready }" aria-hidden="true" />
                <div class="grow">
                  <div class="strong">{{ active.ready ? 'Running on this server' : 'Not answering' }}</div>
                  <code class="small muted">{{ active.url }}</code>
                </div>
                <button class="btn btn-text st" type="button" @click="(newURL = active.url ?? ''), (urlOpen = true)">Address</button>
              </div>
              <M3Expand v-if="active.install && !active.ready">
                <template #summary><span class="small">Install Ollama</span></template>
                <pre class="install">{{ active.install }}</pre>
              </M3Expand>
              <button class="btn btn-outlined st" type="button" @click="loadSettings">Check again</button>
            </template>

            <!-- The CLI has to exist on the server before a token is any use. -->
            <template v-if="active?.kind === 'cli'">
              <M3Expand v-if="active.install && !active.ready">
                <template #summary><span class="small">Install Claude Code</span></template>
                <pre class="install">{{ active.install }}</pre>
              </M3Expand>
              <p v-if="active.ready" class="small muted">
                The panel runs this command with its own tools disabled: every change still waits for your approval here.
              </p>
              <button class="btn btn-outlined st" type="button" @click="loadSettings">Check again</button>
            </template>

            <M3Select v-if="active?.kind === 'cli'" v-model="effort" label="Effort" :options="EFFORTS" :disabled="!settings" />
            <p class="small muted">
              {{ active?.note }}<template v-if="active?.needsKey">{{ ' ' }}{{ active.kind === 'cli' ? 'The token is encrypted on this server.' : 'The key is encrypted on this server.' }}</template>
            </p>

            <h2 id="perm">What the assistant may do</h2>
            <div class="perm">
              <div class="grow"><div>Read the server</div><div class="small muted">Status, logs, metrics, config files</div></div>
              <span class="chip">always</span>
            </div>
            <div class="perm">
              <div class="grow"><div>Run read-only commands without asking</div><div class="small muted">Off: every command waits for you</div></div>
              <M3Switch v-model="autoRead" label="Run read-only commands without asking" :disabled="!settings" />
            </div>
            <div class="perm">
              <div class="grow"><div>Changes</div><div class="small muted">Restart, deploy, any command that modifies: you approve each one</div></div>
              <span class="chip">ask</span>
            </div>
            <div class="never">
              <span class="small muted">Never allowed</span>
              <span v-for="n in NEVER" :key="n" class="never-chip">{{ n }}</span>
            </div>
          </div>
        </M3Expand>
      </section>
    </aside>

    <section class="chat" aria-label="Conversation">
      <header class="chat-head">
        <M3Shape kind="sunny" :size="36" fill="var(--tc)" />
        <h2 class="grow ellipsis">{{ conv?.title ?? 'New conversation' }}</h2>
        <span class="chip chip-outline provider"><span class="dot" :class="{ on: active?.ready }" aria-hidden="true" />{{ label }}</span>
        <span class="chip chip-outline server"><span class="online" aria-hidden="true" />{{ session.host?.hostname ?? '…' }}</span>
      </header>

      <div v-if="active && !active.ready" class="setup">
        <span class="dot warn" aria-hidden="true" />
        <span class="grow small">{{ banner }}</span>
        <button v-if="active.needsKey" class="btn btn-text st" type="button" @click="keyOpen = true">Connect</button>
        <button v-else class="btn btn-text st" type="button" @click="loadSettings">Check again</button>
      </div>

      <div ref="scroller" class="thread" aria-live="polite">
        <div v-if="!conv" class="empty">
          <M3Shape kind="flower" :size="72" fill="var(--pc)" />
          <h3>{{ settings?.configured ? `Ask ${label} about this server` : `Connect ${label}` }}</h3>
          <p class="muted">
            {{ settings?.configured
              ? `${label} reads the server with the tools of this panel. Every change waits for your approval.`
              : active?.hint || `${label} answers with the tools of this panel. Every change waits for your approval.` }}
          </p>
          <div v-if="settings?.configured" class="suggestions">
            <button v-for="s in SUGGESTIONS" :key="s" type="button" class="suggestion st" @click="(input = s), send()">{{ s }}</button>
          </div>
          <button v-else-if="active?.needsKey" class="btn btn-filled btn-lg st" type="button" @click="keyOpen = true">Connect</button>
        </div>

        <template v-for="it in items" :key="it.key">
          <div v-if="it.kind === 'user'" class="bubble user">{{ it.text }}</div>
          <M3Expand v-else-if="it.kind === 'thinking'" class="thinking">
            <template #summary><span class="small muted">Reasoning</span></template>
            <div class="small reasoning" v-html="markdown(it.text ?? '')" />
          </M3Expand>
          <div v-else-if="it.kind === 'text'" class="bubble answer md" v-html="markdown(it.text ?? '')" />
          <div v-else class="tools">
            <template v-for="t in it.tools" :key="t.id">
              <section v-if="t.status === 'pending'" class="approve" :aria-label="`Approval needed: ${t.summary}`">
                <div class="ap-head">
                  <Icon name="security" :size="22" />
                  <h3 class="grow">{{ label }} wants to run an action</h3>
                  <span class="small muted">needs your approval</span>
                </div>
                <code class="cmd">{{ t.summary }}</code>
                <span v-if="JSON.parse(t.input || '{}').purpose" class="small muted">{{ JSON.parse(t.input).purpose }}</span>
                <div class="ap-actions">
                  <button class="btn btn-filled st" type="button" @click="decide(t, true)">Approve</button>
                  <button class="btn btn-outlined st" type="button" @click="decide(t, false)">Deny</button>
                </div>
              </section>
              <M3Expand v-else class="tool" :class="[t.status, { err: t.isError }]">
                <template #summary>
                  <Icon :name="t.status === 'done' && !t.isError ? 'check' : t.status === 'running' ? 'restart' : 'alert'" :size="14" class="ticon" />
                  <code class="ellipsis">{{ t.summary || t.name }}</code>
                  <span class="small tstate">{{ statusLabel(t) }}</span>
                </template>
                <pre v-if="t.result">{{ t.result }}</pre>
              </M3Expand>
            </template>
          </div>
        </template>

        <div v-if="busy && !Object.keys(draft).length" class="working small muted">
          <M3Shape kind="cookie" :size="20" fill="var(--primary)" class="spin" /> {{ label }} is working…
        </div>
        <div v-if="conv?.error" class="note small" :class="{ bad: conv.state === 'error' }">
          <Icon name="info" :size="16" /> {{ conv.error }}
        </div>
      </div>

      <form class="composer" @submit.prevent="send">
        <label class="field compose">
          <textarea
            v-model="input"
            rows="1"
            :placeholder="settings?.configured ? `Ask ${label} about this server` : 'Connect an AI first'"
            :aria-label="`Message ${label}`"
            :disabled="!settings?.configured"
            @keydown="onKey"
          />
          <button v-if="busy" class="send st" type="button" aria-label="Stop" @click="conv && api.stopConversation(conv.id)"><Icon name="stop" :size="18" /></button>
          <button v-else class="send st" type="submit" aria-label="Send" :disabled="!input.trim() || sending || !settings?.configured"><Icon name="send" :size="20" /></button>
        </label>
      </form>
    </section>

    <M3Dialog v-model="keyOpen" :title="keyTitle">
      <p class="muted" style="margin: 0">
        <template v-if="active?.id === 'claude-code'">
          On your own computer, run <code>claude setup-token</code> and paste the token it prints (it starts with
          <code>sk-ant-oat</code>). It bills your Pro or Max subscription instead of an API key. The token is checked
          by the <code>claude</code> command already installed on this server with one small message, then stored
          encrypted here. That command runs with its own tools disabled: every change still waits for your approval.
        </template>
        <template v-else>
          Create a free key at <a href="https://aistudio.google.com/apikey" target="_blank" rel="noopener">AI Studio</a>, no card
          needed. It is checked with Google, then stored encrypted on this server only.
        </template>
      </p>
      <form @submit.prevent="saveKey">
        <label class="field">
          <Icon name="key" :size="20" />
          <input
            v-model="newKey"
            type="password"
            :placeholder="active?.id === 'claude-code' ? 'sk-ant-oat…' : 'AIza…'"
            aria-label="API key"
            autocomplete="off"
          />
        </label>
      </form>
      <template #actions>
        <button v-if="active?.ready" class="btn btn-text st" type="button" :disabled="savingKey" @click="(newKey = ''), saveKey()">Remove key</button>
        <button class="btn btn-filled st" type="button" :disabled="savingKey || !newKey.trim()" @click="saveKey">{{ savingKey ? 'Checking…' : 'Save' }}</button>
      </template>
    </M3Dialog>

    <M3Dialog v-model="urlOpen" title="Ollama address">
      <p class="muted" style="margin: 0">
        Where the panel finds Ollama. Keep it on loopback, or point it at another machine running Ollama.
      </p>
      <form @submit.prevent="saveURL">
        <label class="field">
          <Icon name="server" :size="20" />
          <input v-model="newURL" placeholder="http://127.0.0.1:11434" aria-label="Ollama address" autocomplete="off" />
        </label>
      </form>
      <template #actions>
        <button class="btn btn-text st" type="button" @click="urlOpen = false">Cancel</button>
        <button class="btn btn-filled st" type="button" :disabled="savingURL || !newURL.trim()" @click="saveURL">{{ savingURL ? 'Checking…' : 'Save' }}</button>
      </template>
    </M3Dialog>
  </div>
</template>

<style scoped>
.assistant {
  display: flex;
  gap: 0;
  height: calc(100dvh - 140px);
  border-radius: 28px;
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--sch);
}
.side {
  flex: 0 0 340px;
  display: flex;
  flex-direction: column;
  gap: 12px;
  padding: 20px 18px;
  border-right: 1px solid var(--sch);
  overflow: hidden;
}
.side h1 {
  font-size: 20px;
  margin: 0 4px;
}
.panel {
  border-radius: 24px;
  background: var(--sc);
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.panel h2 {
  font-size: 16px;
}
.panel p {
  margin: 0;
}
.providers {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.prov {
  display: flex;
  align-items: center;
  gap: 10px;
  width: 100%;
  padding: 10px 12px;
  border-radius: 16px;
  border: 1px solid var(--outline-v);
  background: transparent;
  color: var(--on-surface);
  text-align: left;
}
.prov.sel {
  background: var(--sch);
  border-color: var(--primary);
}
.ptext {
  display: flex;
  flex-direction: column;
  gap: 2px;
}
.install {
  margin: 8px 0 0;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--code-bg);
  border: 1px solid var(--code-border);
  color: var(--code-fg);
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
.provider {
  gap: 8px;
}
.key {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 16px;
  background: var(--sch);
}
/* The address is the one value that can be long: it truncates instead of running
   under the button next to it. */
.key > * {
  min-width: 0;
}
.key .btn,
.key > button {
  flex-shrink: 0;
}
.key code {
  display: block;
  max-width: 100%;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}
.strong {
  font-weight: 500;
  font-size: 14px;
}
.dot {
  flex: none;
  width: 10px;
  height: 10px;
  border-radius: 50%;
  background: var(--outline);
}
.dot.on {
  background: var(--ok);
}
.dot.warn {
  background: var(--warn);
}
.perm {
  display: flex;
  align-items: center;
  gap: 12px;
  font-size: 14px;
}
.never {
  display: flex;
  flex-wrap: wrap;
  gap: 6px;
}
.never .muted {
  width: 100%;
}
.never-chip {
  padding: 4px 10px;
  border-radius: 8px;
  border: 1px solid var(--ec);
  color: var(--error);
  font-size: 12px;
}
.history {
  display: flex;
  flex-direction: column;
  gap: 4px;
}
/* The list owns the column's spare height and scrolls on its own, so a long history
   never hides the settings below it, and the settings never hide the list. */
.conv-list {
  flex: 1;
  min-height: 120px;
  overflow-y: auto;
  display: flex;
  flex-direction: column;
  gap: 4px;
  margin: 0 -6px;
  padding: 0 6px;
}
.settings {
  flex: none;
  padding: 8px 10px;
}
.settings-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
  /* Whatever the window height, the open card must leave the list and its own
     summary on screen; past that it scrolls. */
  max-height: clamp(160px, calc(100dvh - 420px), 40dvh);
  overflow-y: auto;
  padding: 6px 6px 4px;
}
.settings-body h2 {
  font-size: 14px;
  margin: 6px 0 0;
}
.sum {
  min-width: 0;
}
.setup {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 24px;
  border-bottom: 1px solid var(--sch);
  background: var(--sch);
}
.setup .btn {
  flex: none;
}
.hist-head {
  display: flex;
  align-items: center;
  justify-content: space-between;
  margin: 4px 4px 6px;
}
.hist-head .btn {
  height: 34px;
  padding: 0 14px;
}
.conv {
  display: flex;
  align-items: center;
  border-radius: 16px;
}
.conv.sel {
  background: var(--sch);
}
.conv-link {
  flex: 1;
  min-width: 0;
  display: flex;
  flex-direction: column;
  padding: 10px 12px;
  border-radius: 16px;
  color: var(--on-surface);
  text-decoration: none;
  font-size: 14px;
}
.chat {
  flex: 1;
  min-width: 0;
  min-height: 0;
  display: flex;
  flex-direction: column;
}
.chat-head {
  display: flex;
  align-items: center;
  gap: 12px;
  padding: 16px 24px;
  border-bottom: 1px solid var(--sch);
}
.chat-head h2 {
  font-size: 18px;
}
.server {
  gap: 8px;
}
.online {
  flex: none;
  width: 8px;
  height: 8px;
  border-radius: 50%;
  background: var(--ok);
}
.thread {
  flex: 1;
  overflow-y: auto;
  padding: 24px;
  display: flex;
  flex-direction: column;
  gap: 14px;
}
.thread > * {
  max-width: 860px;
  width: 100%;
  margin: 0 auto;
}
.empty {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 12px;
  margin: auto;
  text-align: center;
}
.empty h3 {
  font-size: 22px;
  font-weight: 500;
}
.empty p {
  margin: 0;
  max-width: 460px;
}
.suggestions {
  display: flex;
  flex-wrap: wrap;
  justify-content: center;
  gap: 8px;
  margin-top: 8px;
}
.suggestion {
  padding: 10px 16px;
  border-radius: 16px;
  border: 1px solid var(--outline-v);
  background: var(--sc);
  color: var(--on-surface);
  font-size: 14px;
}
.bubble {
  padding: 12px 16px;
  font-size: 15px;
  line-height: 1.6;
}
.bubble.user {
  align-self: flex-end;
  width: auto;
  max-width: min(70%, 600px);
  margin: 0 0 0 auto;
  border-radius: 24px 24px 6px 24px;
  background: var(--pc);
  color: var(--on-pc);
  white-space: pre-wrap;
}
.bubble.answer {
  border-radius: 6px 24px 24px 24px;
  background: var(--sc);
}
.md :deep(p) {
  margin: 0 0 8px;
}
.md :deep(p:last-child) {
  margin: 0;
}
.md :deep(pre) {
  margin: 8px 0;
  padding: 12px;
  border-radius: 12px;
  background: var(--scc);
  overflow-x: auto;
  font-size: 13px;
}
.md :deep(code) {
  font-size: 0.9em;
  color: var(--primary);
}
.md :deep(pre code) {
  color: var(--on-surface);
}
.md :deep(ul),
.md :deep(ol) {
  margin: 4px 0 8px;
  padding-left: 22px;
}
.thinking {
  padding: 0 4px;
}
.thinking :deep(.head) {
  width: auto;
  padding: 4px 8px;
}
.reasoning {
  padding: 8px 12px;
  border-left: 2px solid var(--outline-v);
  margin: 6px 0 0 8px;
  color: var(--on-sv);
}
.tools {
  display: flex;
  flex-direction: column;
  gap: 6px;
}
.tool {
  border-radius: 10px;
  background: var(--sc);
  border: 1px solid var(--outline-v);
  font-size: 13px;
}
.tool :deep(.head) {
  gap: 8px;
  padding: 6px 12px;
  color: var(--on-sv);
}
.tool :deep(.chev) {
  margin-left: 0;
}
.ticon {
  flex-shrink: 0;
  color: var(--ok);
}
.tool.err .ticon,
.tool.denied .ticon,
.tool.blocked .ticon {
  color: var(--error);
}
.tool.running .ticon {
  color: var(--primary);
  animation: spin 1.2s linear infinite;
}
.tstate {
  flex-shrink: 0;
}
.tool code {
  flex: 1;
  min-width: 0;
  color: var(--on-surface);
}
.tool pre {
  margin: 0;
  padding: 10px 12px;
  border-top: 1px solid var(--outline-v);
  max-height: 280px;
  overflow: auto;
  font-size: 12px;
  white-space: pre-wrap;
  word-break: break-all;
}
.approve {
  border-radius: 24px;
  border: 2px solid var(--primary);
  background: var(--scl);
  padding: 16px 18px;
  display: flex;
  flex-direction: column;
  gap: 10px;
}
.ap-head {
  display: flex;
  align-items: center;
  gap: 10px;
  color: var(--primary);
}
.ap-head h3 {
  font-size: 16px;
  font-weight: 500;
  color: var(--on-surface);
}
.cmd {
  display: block;
  padding: 12px;
  border-radius: 12px;
  background: var(--code-bg);
  border: 1px solid var(--code-border);
  color: var(--code-fg);
  font-size: 13px;
  white-space: pre-wrap;
  word-break: break-all;
}
.ap-actions {
  display: flex;
  gap: 8px;
}
.working {
  display: flex;
  align-items: center;
  gap: 8px;
}
.spin {
  animation: spin 2s linear infinite;
}
@keyframes spin {
  to {
    rotate: 360deg;
  }
}
.note {
  display: flex;
  gap: 8px;
  padding: 10px 12px;
  border-radius: 12px;
  background: var(--sch);
}
.note.bad {
  background: var(--ec);
  color: var(--on-ec);
}
.composer {
  padding: 12px 24px 20px;
}
.compose {
  max-width: 860px;
  margin: 0 auto;
  height: auto;
  min-height: 56px;
  padding: 6px 8px 6px 20px;
  border-radius: 28px;
}
.compose textarea {
  flex: 1;
  border: none;
  background: none;
  outline: none;
  resize: none;
  max-height: 200px;
  field-sizing: content;
  font-size: 15px;
  color: var(--on-surface);
  padding: 8px 0;
}
.send {
  width: 44px;
  height: 44px;
  border-radius: 22px;
  border: none;
  background: var(--primary);
  color: var(--on-primary);
  display: grid;
  place-items: center;
  flex-shrink: 0;
}
/* An empty composer leaves the button disabled, and the half opacity every disabled button
   gets in the panel washed the arrow into its own circle (3.1:1). The muted pair keeps the
   shape and still reads as not ready yet. */
.send:disabled {
  opacity: 1;
  background: var(--sch);
  color: var(--on-sv);
}
@media (min-width: 1001px) {
  :global(html.assistant-page) {
    overflow: hidden;
  }
}
@media (max-width: 1000px) {
  .assistant {
    height: auto;
    flex-direction: column;
    border: none;
    background: transparent;
  }
  .side {
    flex: none;
    max-height: none;
    border-right: none;
    padding: 0;
  }
  .conv-list {
    max-height: 40dvh;
  }
  .chat {
    max-height: none;
    min-height: 70vh;
    border-radius: 24px;
    background: var(--surface);
    border: 1px solid var(--sch);
  }
}
</style>
