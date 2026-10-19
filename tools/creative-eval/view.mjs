import { spawn } from 'node:child_process'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const directory = fileURLToPath(new URL('.', import.meta.url))
const child = spawn(process.execPath, [path.join(directory, 'node_modules/promptfoo/dist/src/entrypoint.js'),
  'view', ...process.argv.slice(2)], {
  stdio: 'inherit',
  env: { ...process.env, PROMPTFOO_DISABLE_TELEMETRY: '1', PROMPTFOO_CONFIG_DIR: path.join(directory, 'results', 'promptfoo') },
})
child.on('error', error => { console.error(error.message); process.exitCode = 1 })
child.on('exit', code => { process.exitCode = code ?? 1 })
for (const signal of ['SIGINT', 'SIGTERM']) process.once(signal, () => child.kill(signal))
