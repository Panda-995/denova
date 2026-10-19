import { connect } from './client.mjs';
const client = await connect();
const text = document.querySelector('#text'), status = document.querySelector('#status');
let labels = {}, revision = null, dirty = false;
async function appearance() {
  labels = await (await fetch(`./locales/${client.context.locale}.json`)).json();
  document.documentElement.dataset.theme = client.context.theme;
  document.documentElement.lang = client.context.locale;
  for (const el of document.querySelectorAll('[data-label]')) el.textContent = labels[el.dataset.label];
}
await appearance(); window.addEventListener('denova:appearance', appearance);
text.oninput = () => { dirty = true; client.setState({ dirty }); };
async function perform(work) {
  client.setState({ busy: true, dirty });
  for (const button of document.querySelectorAll('button')) button.disabled = true;
  try { await work(); status.textContent = labels['panel.done']; }
  catch (error) { status.textContent = labels[error.code === 'DOCUMENT_CONFLICT' ? 'panel.conflict' : 'errors.requestFailed']; console.error(error); }
  finally { client.setState({ dirty }); for (const button of document.querySelectorAll('button')) button.disabled = false; }
}
async function load() {
  let file;
  try { file = await client.request('/assets/document?path=draft.json'); }
  catch (error) { if (error.code !== 'NOT_FOUND') throw error; }
  const data = file ? JSON.parse(file.content) : { format: 1, text: '' };
  if (data.format !== 1 || typeof data.text !== 'string') throw new Error('Unsupported draft format');
  text.value = data.text; revision = file?.revision ?? null; dirty = false;
}
document.querySelector('#reload').onclick = () => {
  if (!dirty || confirm(labels['panel.discard'])) void perform(load);
};
document.querySelector('#save').onclick = () => perform(async () => {
  const result = await client.request('/assets/document', { method: 'PUT', body: JSON.stringify({ path: 'draft.json', content: JSON.stringify({ format: 1, text: text.value }), expectedRevision: revision }) });
  revision = result.revision; dirty = false;
});
document.querySelector('#echo').onclick = () => perform(async () => {
  const result = await client.request(`/tools/${client.context.source.package.id}/echo/invoke`, { method: 'POST', body: JSON.stringify({ input: { text: text.value } }) });
  document.querySelector('#result').textContent = result.content;
});
await perform(load);
