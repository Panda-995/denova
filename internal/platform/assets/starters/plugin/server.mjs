import { readJSON, serve, requestHost } from './runtime.mjs';

// Replace this minimal handler and its tool definition with your plugin's behavior.
// The host handles authentication, lifecycle and tool-input validation.
await serve(async (request, response, bootstrap) => {
  if (request.method === 'POST' && request.url === '/tools/read-draft') {
    const result = await requestHost(bootstrap, request, '/assets/document?path=draft.json');
    if (!result.ok && result.status !== 404) { response.writeHead(result.status, { 'Content-Type': 'application/json' }).end(await result.text()); return; }
    const file = result.ok ? await result.json() : null;
    const draft = file ? JSON.parse(file.content) : { format: 1, text: '' };
    if (draft.format !== 1 || typeof draft.text !== 'string') throw new Error('Unsupported draft format');
    response.writeHead(200, { 'Content-Type': 'application/json' }).end(JSON.stringify({ content: draft.text, data: { text: draft.text } })); return;
  }
  if (request.method === 'POST' && request.url === '/tools/echo') {
    const { text } = await readJSON(request);
    response.writeHead(200, { 'Content-Type': 'application/json' });
    response.end(JSON.stringify({ content: text, data: { text } }));
    return;
  }
  response.writeHead(404, { 'Content-Type': 'application/json' });
  response.end(JSON.stringify({ code: 'NOT_FOUND', messageKey: 'errors.notFound', diagnostic: 'Unknown plugin endpoint' }));
});
