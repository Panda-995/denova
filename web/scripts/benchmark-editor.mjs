import { readFileSync, mkdirSync, writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { performance } from 'node:perf_hooks'
import ts from 'typescript'

// Compile the actual pure production function, without loading a DOM/editor or
// introducing a production-only test entry point. The reference is its prior implementation.
const path = new URL('../src/components/Editor/editorDocument.ts', import.meta.url)
const source = ts.createSourceFile(fileURLToPath(path), readFileSync(path, 'utf8'), ts.ScriptTarget.Latest)
const declaration = source.statements.find(node => ts.isFunctionDeclaration(node) && node.name?.text === 'countTextCharacters')
if (!declaration) throw new Error('Character counter was not found')
const compiled = ts.transpileModule(declaration.getText(source).replace(/^export\s+/, ''), {
  compilerOptions: { target: ts.ScriptTarget.ES2022 },
}).outputText
const current = new Function(`${compiled}\nreturn countTextCharacters`)()
const previous = text => Array.from(text.replace(/\s/g, '')).length
const text = '中文叙事 English 😀段落。\n'.repeat(10_000)
if (current(text) !== previous(text)) throw new Error('Character count changed')

function measure(counter) {
  for (let index = 0; index < 3; index++) counter(text)
  const samples = []
  for (let sample = 0; sample < 7; sample++) {
    const start = performance.now()
    for (let iteration = 0; iteration < 20; iteration++) counter(text)
    samples.push((performance.now() - start) / 20)
  }
  return samples
}

const before = measure(previous)
const after = measure(current)
const median = samples => [...samples].sort((left, right) => left - right)[Math.floor(samples.length / 2)]
const report = { platform: process.platform, utf16Units: text.length, beforeMs: before, afterMs: after, speedup: median(before) / median(after) }
const output = new URL('../test-results/performance-editor.json', import.meta.url)
mkdirSync(new URL('../test-results/', import.meta.url), { recursive: true })
writeFileSync(output, JSON.stringify(report, null, 2) + '\n')
console.log(JSON.stringify({ beforeMs: median(before), afterMs: median(after), speedup: report.speedup }))
