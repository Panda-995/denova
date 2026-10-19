export function structuralChecks(_output, context) {
  const checks = context.providerResponse?.metadata?.checks
  if (!checks?.length) return { pass: false, score: 0, reason: 'Missing persisted evidence; the scenario is incomplete' }
  const failed = checks.filter(check => !check.pass)
  return { pass: failed.length === 0, score: failed.length ? 0 : 1,
    reason: failed.length ? failed.map(check => check.name).join('; ') : checks.map(check => check.name).join('; ') }
}
