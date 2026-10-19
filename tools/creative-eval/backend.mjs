import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { createWriteStream } from 'node:fs'
import { mkdir, writeFile } from 'node:fs/promises'
import http from 'node:http'
import net from 'node:net'
import path from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'

// Own only this evaluation's child process. No development server is reused.
export async function startBackend({ binary, directory, configuration, skillsDirectory, signal }) {
  await mkdir(directory, { recursive: true, mode: 0o700 })
  const dataDirectory = path.join(directory, '.denova')
  await mkdir(dataDirectory, { mode: 0o700 })
  const configPath = path.join(dataDirectory, 'config.toml')
  await writeFile(configPath, configuration, { mode: 0o600 })
  const reservation = net.createServer()
  reservation.listen(0, '127.0.0.1')
  await once(reservation, 'listening')
  const port = reservation.address().port
  await new Promise(resolve => reservation.close(resolve))
  const environment = Object.fromEntries(Object.entries(process.env)
    .filter(([key]) => !/^(DENOVA_|NOVA_)/.test(key)))
  const log = createWriteStream(path.join(directory, 'backend.log'), { mode: 0o600 })
  const child = spawn(binary, ['--no-open', '--dev-mode', `--port=${port}`], {
    cwd: directory,
    env: { ...environment, DENOVA_DIR: dataDirectory, DENOVA_SKILLS_DIR: skillsDirectory },
    stdio: ['ignore', 'pipe', 'pipe'],
  })
  child.stdout.pipe(log, { end: false })
  child.stderr.pipe(log, { end: false })
  let spawnError
  child.on('error', error => { spawnError = error })
  const exited = new Promise(resolve => child.once('close', resolve))
  const abort = () => child.kill('SIGTERM')
  signal?.addEventListener('abort', abort, { once: true })
  const stop = async () => {
    signal?.removeEventListener('abort', abort)
    if (child.exitCode === null && child.signalCode === null) {
      child.kill('SIGTERM')
      const force = setTimeout(() => child.kill('SIGKILL'), 10_000)
      try { await exited } finally { clearTimeout(force) }
    }
    log.end()
  }
  const baseURL = `http://127.0.0.1:${port}`
  try {
    const deadline = Date.now() + 60_000 // Startup infrastructure, not an Agent runtime limit.
    while (true) {
      signal?.throwIfAborted()
      if (spawnError) throw spawnError
      if (child.exitCode !== null || child.signalCode !== null) throw new Error('Evaluation backend exited during startup; inspect backend.log')
      try {
        await request(baseURL, '/api/status', { signal: AbortSignal.timeout(1000) })
        break
      } catch (error) {
        if (Date.now() >= deadline) throw new Error('Evaluation backend startup timed out; inspect backend.log', { cause: error })
        await delay(200, undefined, { signal })
      }
    }
    return { baseURL, stop }
  } catch (error) {
    await stop()
    throw error
  }
}

// http.request has no total response deadline, so long model/tool runs remain
// cancellable without imposing a fixed LLM runtime limit.
export async function request(baseURL, route, { body, signal, onEvent } = {}) {
  const response = await new Promise((resolve, reject) => {
    const outgoing = http.request(new URL(route, baseURL), {
      method: body === undefined ? 'GET' : 'POST', signal, agent: false,
      headers: { 'Content-Type': 'application/json', 'Accept-Language': 'en-US' },
    }, resolve)
    outgoing.on('error', reject)
    outgoing.end(body === undefined ? undefined : JSON.stringify(body))
  })
  response.setEncoding('utf8')
  let pending = ''
  const streaming = response.headers['content-type']?.includes('text/event-stream')
  for await (const chunk of response) {
    pending += chunk
    if (streaming && response.statusCode < 400) {
      let end
      while ((end = pending.indexOf('\n\n')) !== -1) {
        const block = pending.slice(0, end)
        pending = pending.slice(end + 2)
        const data = block.split('\n').filter(line => line.startsWith('data:')).map(line => line.slice(5).trim()).join('\n')
        if (!data || data === '[DONE]') continue
        const event = block.match(/^event: ?(.+)$/m)?.[1]
        onEvent?.({ event, data: JSON.parse(data) })
      }
    }
  }
  if (response.statusCode >= 400) throw new Error(`${route}: HTTP ${response.statusCode}: ${pending}`)
  if (streaming) {
    if (pending.trim()) throw new Error(`${route}: incomplete event stream`)
    return
  }
  return pending ? JSON.parse(pending) : undefined
}
