<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import Icon from '@/components/Icon.vue'
import M3Shape from '@/components/M3Shape.vue'
import M3Switch from '@/components/M3Switch.vue'
import M3Dialog from '@/components/M3Dialog.vue'
import {
  api,
  streamConversation,
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
const models = ref<{ id: string; label: string }[]>([])
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
const scroller = ref<HTMLElement>()
let close: (() => void) | undefined

const NEVER = ['Delete Docker volumes', 'Change SSH or the firewall', 'Read keys and secrets', 'Power off the server']
const busy = computed(() => conv.value?.state === 'running')

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

async function saveKey() {
  savingKey.value = true
  try {
    const res = await api.assistantUpdate({ apiKey: newKey.value.trim() })
    settings.value = res.settings
    newKey.value = ''
    keyOpen.value = false
    toast(res.settings.configured ? 'Anthropic account connected' : 'API key removed')
  } catch (e) {
    toastError(e)
  } finally {
    savingKey.value = false
  }
}

async function setOption(o: { model?: string; effort?: string; autoRead?: boolean }) {
  try {
    settings.value = (await api.assistantUpdate(o)).settings
  } catch (e) {
    toastError(e)
  }
}
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
  try {
    await Promise.all([loadSettings(), loadList()])
  } catch (e) {
    toastError(e)
  }
  open(props.id)
})
onBeforeUnmount(() => close?.())
</script>

<template>
  <div class="assistant">
    <aside class="side" aria-label="Assistant settings">
      <h1>Assistant</h1>

      <section class="panel" aria-labelledby="acct">
        <h2 id="acct">Anthropic account</h2>
        <div class="key">
          <span class="dot" :class="{ on: settings?.configured }" aria-hidden="true" />
          <div class="grow">
            <div class="strong">{{ settings?.configured ? 'API key connected' : 'Not connected' }}</div>
            <code v-if="settings?.keyHint" class="small muted">{{ settings.keyHint }}</code>
          </div>
          <button class="btn btn-text st" type="button" @click="keyOpen = true">{{ settings?.configured ? 'Change' : 'Connect' }}</button>
        </div>
        <label class="tf">
          <span>Model</span>
          <select :value="settings?.model" :disabled="!settings" @change="setOption({ model: ($event.target as HTMLSelectElement).value })">
            <option v-for="m in models" :key="m.id" :value="m.id">{{ m.label }}</option>
          </select>
        </label>
        <label class="tf">
          <span>Effort</span>
          <select :value="settings?.effort" :disabled="!settings" @change="setOption({ effort: ($event.target as HTMLSelectElement).value })">
            <option value="low">Low: quick answers</option>
            <option value="medium">Medium</option>
            <option value="high">High: careful investigation</option>
            <option value="xhigh">Extra high</option>
            <option value="max">Max</option>
          </select>
        </label>
        <p class="small muted">The key is encrypted on this server. Usage is billed to your Anthropic account.</p>
      </section>

      <section class="panel" aria-labelledby="perm">
        <h2 id="perm">What Claude may do</h2>
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
      </section>

      <section class="history" aria-labelledby="hist">
        <div class="hist-head">
          <h2 id="hist">Conversations</h2>
          <RouterLink to="/assistant" class="btn btn-tonal st"><Icon name="plus" :size="18" /> New</RouterLink>
        </div>
        <p v-if="!conversations.length" class="small muted">None yet.</p>
        <div v-for="c in conversations" :key="c.id" class="conv" :class="{ sel: c.id === conv?.id }">
          <RouterLink :to="`/assistant/${c.id}`" class="conv-link st">
            <span class="ellipsis">{{ c.title }}</span>
            <span class="small muted">{{ c.state === 'awaiting' ? 'waiting for you' : ago(c.updatedAt) }}</span>
          </RouterLink>
          <button class="icon-btn st" type="button" :aria-label="`Delete ${c.title}`" @click="remove(c)"><Icon name="trash" :size="18" /></button>
        </div>
      </section>
    </aside>

    <section class="chat" aria-label="Conversation">
      <header class="chat-head">
        <M3Shape kind="sunny" :size="36" fill="var(--tc)" />
        <h2 class="grow ellipsis">{{ conv?.title ?? 'New conversation' }}</h2>
        <span class="chip chip-outline server"><span class="online" aria-hidden="true" />{{ session.host?.hostname ?? '…' }}</span>
      </header>

      <div ref="scroller" class="thread" aria-live="polite">
        <div v-if="!conv" class="empty">
          <M3Shape kind="flower" :size="72" fill="var(--pc)" />
          <h3>{{ settings?.configured ? 'Ask Claude about this server' : 'Connect your Anthropic account' }}</h3>
          <p class="muted">
            {{ settings?.configured
              ? 'Claude reads the server with the tools of this panel. Every change waits for your approval.'
              : 'Paste an API key from console.anthropic.com to let Claude look into this server with you.' }}
          </p>
          <div v-if="settings?.configured" class="suggestions">
            <button v-for="s in SUGGESTIONS" :key="s" type="button" class="suggestion st" @click="(input = s), send()">{{ s }}</button>
          </div>
          <button v-else class="btn btn-filled btn-lg st" type="button" @click="keyOpen = true">Connect</button>
        </div>

        <template v-for="it in items" :key="it.key">
          <div v-if="it.kind === 'user'" class="bubble user">{{ it.text }}</div>
          <details v-else-if="it.kind === 'thinking'" class="thinking">
            <summary class="small muted">Reasoning</summary>
            <div class="small" v-html="markdown(it.text ?? '')" />
          </details>
          <div v-else-if="it.kind === 'text'" class="bubble claude md" v-html="markdown(it.text ?? '')" />
          <div v-else class="tools">
            <template v-for="t in it.tools" :key="t.id">
              <section v-if="t.status === 'pending'" class="approve" :aria-label="`Approval needed: ${t.summary}`">
                <div class="ap-head">
                  <Icon name="security" :size="22" />
                  <h3 class="grow">Claude wants to run an action</h3>
                  <span class="small muted">needs your approval</span>
                </div>
                <code class="cmd">{{ t.summary }}</code>
                <span v-if="JSON.parse(t.input || '{}').purpose" class="small muted">{{ JSON.parse(t.input).purpose }}</span>
                <div class="ap-actions">
                  <button class="btn btn-filled st" type="button" @click="decide(t, true)">Approve</button>
                  <button class="btn btn-outlined st" type="button" @click="decide(t, false)">Deny</button>
                </div>
              </section>
              <details v-else class="tool" :class="[t.status, { err: t.isError }]">
                <summary>
                  <Icon :name="t.status === 'done' && !t.isError ? 'check' : t.status === 'running' ? 'restart' : 'alert'" :size="14" />
                  <code class="ellipsis">{{ t.summary || t.name }}</code>
                  <span class="small">{{ statusLabel(t) }}</span>
                </summary>
                <pre v-if="t.result">{{ t.result }}</pre>
              </details>
            </template>
          </div>
        </template>

        <div v-if="busy && !Object.keys(draft).length" class="working small muted">
          <M3Shape kind="cookie" :size="20" fill="var(--primary)" class="spin" /> Claude is working…
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
            :placeholder="settings?.configured ? 'Ask Claude about this server' : 'Connect an API key first'"
            aria-label="Message Claude"
            :disabled="!settings?.configured"
            @keydown="onKey"
          />
          <button v-if="busy" class="send st" type="button" aria-label="Stop" @click="conv && api.stopConversation(conv.id)"><Icon name="stop" :size="18" /></button>
          <button v-else class="send st" type="submit" aria-label="Send" :disabled="!input.trim() || sending || !settings?.configured"><Icon name="send" :size="20" /></button>
        </label>
      </form>
    </section>

    <M3Dialog v-model="keyOpen" title="Anthropic API key">
      <p class="muted" style="margin: 0">
        Create a key at <a href="https://console.anthropic.com/settings/keys" target="_blank" rel="noopener">console.anthropic.com</a>
        and paste it here. It is checked with Anthropic, then stored encrypted on this server only.
      </p>
      <form @submit.prevent="saveKey">
        <label class="field">
          <Icon name="key" :size="20" />
          <input v-model="newKey" type="password" placeholder="sk-ant-…" aria-label="API key" autocomplete="off" />
        </label>
      </form>
      <template #actions>
        <button v-if="settings?.configured" class="btn btn-text st" type="button" :disabled="savingKey" @click="(newKey = ''), saveKey()">Remove key</button>
        <button class="btn btn-filled st" type="button" :disabled="savingKey || !newKey.trim()" @click="saveKey">{{ savingKey ? 'Checking…' : 'Save' }}</button>
      </template>
    </M3Dialog>
  </div>
</template>

<style scoped>
.assistant {
  display: flex;
  gap: 0;
  min-height: calc(100vh - 140px);
  border-radius: 28px;
  overflow: hidden;
  background: var(--surface);
  border: 1px solid var(--sch);
}
.side {
  flex: 0 0 340px;
  display: flex;
  flex-direction: column;
  gap: 14px;
  padding: 20px 18px;
  border-right: 1px solid var(--sch);
  overflow-y: auto;
  max-height: calc(100vh - 140px);
}
.side h1 {
  font-size: 26px;
  margin: 0 4px 4px;
}
.panel {
  border-radius: 24px;
  background: var(--sc);
  padding: 16px;
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.panel h2,
.history h2 {
  font-size: 16px;
}
.panel p {
  margin: 0;
}
.key {
  display: flex;
  align-items: center;
  gap: 10px;
  padding: 10px 12px;
  border-radius: 16px;
  background: var(--sch);
}
.strong {
  font-weight: 500;
  font-size: 14px;
}
.dot {
  width: 10px;
  height: 10px;
  border-radius: 5px;
  background: var(--outline);
}
.dot.on {
  background: var(--ok);
}
.tf {
  display: flex;
  flex-direction: column;
  gap: 2px;
  padding: 8px 14px;
  border-radius: 12px 12px 4px 4px;
  background: var(--sch);
  border-bottom: 2px solid var(--on-sv);
}
.tf span {
  font-size: 12px;
  font-weight: 500;
  color: var(--primary);
}
.tf select {
  border: none;
  background: none;
  font-size: 15px;
  color: var(--on-surface);
  padding: 2px 0;
}
.tf option {
  background: var(--sch);
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
  display: flex;
  flex-direction: column;
  max-height: calc(100vh - 140px);
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
  width: 8px;
  height: 8px;
  border-radius: 4px;
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
.bubble.claude {
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
.thinking summary {
  cursor: pointer;
}
.thinking div {
  padding: 8px 12px;
  border-left: 2px solid var(--outline-v);
  margin-top: 6px;
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
.tool summary {
  display: flex;
  align-items: center;
  gap: 8px;
  padding: 6px 12px;
  cursor: pointer;
  color: var(--on-sv);
}
.tool summary svg {
  flex-shrink: 0;
  color: var(--ok);
}
.tool.err summary svg,
.tool.denied summary svg,
.tool.blocked summary svg {
  color: var(--error);
}
.tool.running summary svg {
  color: var(--primary);
  animation: spin 1.2s linear infinite;
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
  background: var(--scc);
  color: var(--primary);
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
@media (max-width: 1000px) {
  .assistant {
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
  .chat {
    max-height: none;
    min-height: 70vh;
    border-radius: 24px;
    background: var(--surface);
    border: 1px solid var(--sch);
  }
}
</style>
