// POST /api/ai: runs a model request the engine built (internal/ai's
// prompts) against the Anthropic API with this project's ANTHROPIC_API_KEY,
// the way internal/ai/anthropic.go does: cached system prefix, effort per
// route, structured output when a schema is given, server-side fallback.
//
// Without stream it answers with the engine's Response JSON. With stream it
// sends server-sent events: delta {text} as text arrives, then done with
// the Response, or error {message}.
import { error, guard, json } from '../_lib/http.js'

const API = 'https://api.anthropic.com/v1/messages'
const EFFORTS = new Set(['low', 'medium', 'high'])

function cached(texts, ttl) {
  const blocks = (texts || []).filter((t) => typeof t === 'string' && t.trim()).map((text) => ({ type: 'text', text }))
  if (blocks.length) blocks[blocks.length - 1].cache_control = { type: 'ephemeral', ttl }
  return blocks
}

function params(req, env, user) {
  const p = {
    model: env.AI_MODEL || req.model || 'claude-opus-5-5',
    max_tokens: Math.min(Number(req.maxTokens) || 4096, 8192),
    messages: [{ role: 'user', content: [{ type: 'text', text: String(req.prompt || '') }] }],
    system: [...cached(req.system, '1h'), ...cached(req.context, '5m')],
    output_config: { effort: EFFORTS.has(req.effort) ? req.effort : 'medium' },
    metadata: { user_id: user.email },
    fallbacks: 'default',
  }
  if (req.schema) p.output_config.format = { type: 'json_schema', schema: req.schema }
  return p
}

function response(req, msg, text) {
  const r = {
    text,
    model: msg.model,
    stopReason: msg.stop_reason,
    usage: {
      inputTokens: msg.usage?.input_tokens ?? 0,
      outputTokens: msg.usage?.output_tokens ?? 0,
      cacheReadTokens: msg.usage?.cache_read_input_tokens ?? 0,
      cacheWriteTokens: msg.usage?.cache_creation_input_tokens ?? 0,
    },
  }
  if (msg.stop_reason === 'refusal') throw new Error('The model declined this request.')
  if (req.schema) r.json = JSON.parse(text)
  return r
}

async function call(env, body) {
  return fetch(API, {
    method: 'POST',
    headers: {
      'x-api-key': env.ANTHROPIC_API_KEY,
      'anthropic-version': '2023-06-01',
      'anthropic-beta': 'server-side-fallback-2026-07-01',
      'content-type': 'application/json',
    },
    body: JSON.stringify(body),
  })
}

export async function onRequestPost({ request, env }) {
  const [user, refused] = await guard(request, env)
  if (refused) return refused
  if (!env.ANTHROPIC_API_KEY) return error('AI isn’t set up for this deployment.', 503)
  let req
  try {
    req = await request.json()
  } catch {
    return error('send JSON', 400)
  }
  const body = params(req, env, user)

  if (!req.stream) {
    const res = await call(env, body)
    if (!res.ok) return error(`The AI request failed (${res.status}).`, 502)
    const msg = await res.json()
    const text = (msg.content || []).filter((b) => b.type === 'text').map((b) => b.text).join('')
    try {
      return json(response(req, msg, text))
    } catch (e) {
      return error(e.message, 502)
    }
  }

  const upstream = await call(env, { ...body, stream: true })
  if (!upstream.ok) return error(`The AI request failed (${upstream.status}).`, 502)
  const { readable, writable } = new TransformStream()
  const writer = writable.getWriter()
  const enc = new TextEncoder()
  const send = (event, data) => writer.write(enc.encode(`event: ${event}\ndata: ${JSON.stringify(data)}\n\n`))
  ;(async () => {
    const reader = upstream.body.getReader()
    const dec = new TextDecoder()
    let buf = ''
    let text = ''
    const msg = { usage: {} }
    try {
      for (;;) {
        const { value, done } = await reader.read()
        if (done) break
        buf += dec.decode(value, { stream: true })
        let i
        while ((i = buf.indexOf('\n\n')) >= 0) {
          const block = buf.slice(0, i)
          buf = buf.slice(i + 2)
          const data = /^data: (.*)$/m.exec(block)?.[1]
          if (!data) continue
          const ev = JSON.parse(data)
          if (ev.type === 'message_start') Object.assign(msg, ev.message, { usage: { ...ev.message.usage } })
          else if (ev.type === 'content_block_delta' && ev.delta?.type === 'text_delta') {
            text += ev.delta.text
            await send('delta', { text: ev.delta.text })
          } else if (ev.type === 'message_delta') {
            if (ev.delta?.stop_reason) msg.stop_reason = ev.delta.stop_reason
            Object.assign(msg.usage, ev.usage || {})
          } else if (ev.type === 'error') throw new Error(ev.error?.message || 'the model stream failed')
        }
      }
      await send('done', response(req, msg, text))
    } catch (e) {
      await send('error', { message: e.message || 'The AI request failed.' })
    } finally {
      await writer.close()
    }
  })()
  return new Response(readable, { headers: { 'Content-Type': 'text/event-stream', 'Cache-Control': 'no-store' } })
}
