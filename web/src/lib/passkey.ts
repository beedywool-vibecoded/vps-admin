// WebAuthn ceremonies. The agent sends options with base64url fields (go-webauthn) and
// reads the credential back as JSON in the same encoding.
import { request } from './api'

interface Options {
  ceremony: string
  publicKey: Record<string, unknown>
}

function fromB64url(s: string): ArrayBuffer {
  const b64 = s.replace(/-/g, '+').replace(/_/g, '/').padEnd(Math.ceil(s.length / 4) * 4, '=')
  const bin = atob(b64)
  const out = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i)
  return out.buffer
}

function toB64url(buf: ArrayBuffer | ArrayBufferView): string {
  const bytes = buf instanceof ArrayBuffer ? new Uint8Array(buf) : new Uint8Array(buf.buffer, buf.byteOffset, buf.byteLength)
  let bin = ''
  for (const b of bytes) bin += String.fromCharCode(b)
  return btoa(bin).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
}

type Descriptor = { id: string; type: string; transports?: string[] }

function decodeDescriptors(list: unknown): PublicKeyCredentialDescriptor[] | undefined {
  if (!Array.isArray(list)) return undefined
  return (list as Descriptor[]).map((d) => ({
    ...d,
    id: fromB64url(d.id),
    transports: d.transports as AuthenticatorTransport[] | undefined,
  })) as PublicKeyCredentialDescriptor[]
}

function credentialJSON(cred: PublicKeyCredential): unknown {
  // Native toJSON exists in current browsers; the manual path covers older ones.
  const native = (cred as unknown as { toJSON?: () => unknown }).toJSON
  if (typeof native === 'function') return native.call(cred)
  const r = cred.response as AuthenticatorAttestationResponse & AuthenticatorAssertionResponse
  const response: Record<string, unknown> = { clientDataJSON: toB64url(r.clientDataJSON) }
  if ('attestationObject' in r && r.attestationObject) {
    response.attestationObject = toB64url(r.attestationObject)
    response.transports = r.getTransports?.() ?? []
  } else {
    response.authenticatorData = toB64url(r.authenticatorData)
    response.signature = toB64url(r.signature)
    if (r.userHandle) response.userHandle = toB64url(r.userHandle)
  }
  return {
    id: cred.id,
    rawId: toB64url(cred.rawId),
    type: cred.type,
    response,
    clientExtensionResults: cred.getClientExtensionResults(),
    authenticatorAttachment: cred.authenticatorAttachment ?? undefined,
  }
}

export const passkeysSupported = () =>
  typeof window !== 'undefined' && 'PublicKeyCredential' in window && window.isSecureContext

export async function registerPasskey(): Promise<void> {
  const opts = await request<Options>('POST', '/api/auth/passkey/register/begin')
  const pk = opts.publicKey as Record<string, any>
  const cred = (await navigator.credentials.create({
    publicKey: {
      ...pk,
      challenge: fromB64url(pk.challenge),
      user: { ...pk.user, id: fromB64url(pk.user.id) },
      excludeCredentials: decodeDescriptors(pk.excludeCredentials),
    } as PublicKeyCredentialCreationOptions,
  })) as PublicKeyCredential | null
  if (!cred) throw new Error('Passkey creation was cancelled')
  await request('POST', `/api/auth/passkey/register/finish?ceremony=${encodeURIComponent(opts.ceremony)}`, credentialJSON(cred))
}

export async function loginWithPasskey(): Promise<void> {
  const opts = await request<Options>('POST', '/api/auth/passkey/login/begin')
  const pk = opts.publicKey as Record<string, any>
  const cred = (await navigator.credentials.get({
    publicKey: {
      ...pk,
      challenge: fromB64url(pk.challenge),
      allowCredentials: decodeDescriptors(pk.allowCredentials),
    } as PublicKeyCredentialRequestOptions,
  })) as PublicKeyCredential | null
  if (!cred) throw new Error('Sign-in was cancelled')
  await request('POST', `/api/auth/passkey/login/finish?ceremony=${encodeURIComponent(opts.ceremony)}`, credentialJSON(cred))
}
