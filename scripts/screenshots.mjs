// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds three years of a household's money):
//
//   DATA_DIR=/tmp/wealth-demo DEMO_DATA=true ADMIN_EMAIL=demo@example.com ADMIN_PASSWORD=demodemo1 ADMIN_NAME=Maya PORT=18331 ./pocket-wealth
//   CDP=http://127.0.0.1:9333 BASE=http://127.0.0.1:18331 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                 # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18331'
const shots = process.env.SHOTS !== '0'
const dir = new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const res = await page.evaluate(() => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'demo@example.com', password: 'demodemo1' }) }).then(r => r.status))
  assert.equal(res, 200, 'demo sign-in')
  return { ctx, page }
}
async function go(page, path, wait = 1200, frozen = true) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  if (frozen) await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name, full = false) { if (shots) await page.screenshot({ path: dir + name + '.png', fullPage: full }) }

// asset ids from the demo: the broker account (4), the flat (7), the mortgage (11)
// ---- desktop ----
{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok((await page.content()).includes('Net worth over time'), 'the overview shows the history')
  await page.mouse.move(900, 520)
  await page.waitForTimeout(300)
  await shot(page, 'desktop-overview')
  await go(page, '/assets')
  await shot(page, 'desktop-assets')
  await go(page, '/a/4')
  await shot(page, 'desktop-broker')
  await go(page, '/investments')
  await shot(page, 'desktop-investments')
  await go(page, '/plan')
  await shot(page, 'desktop-plan')
  await go(page, '/a/11')
  await shot(page, 'desktop-mortgage')
  for (const p of ['/debts', '/connect', '/a/7', '/a/8', '/assets?group=cash', '/settings/apps', '/settings/notifications', '/settings/general', '/settings/ai', '/settings/data', '/settings/about']) await go(page, p, 400)
  await ctx.close()
}

// ---- phone ----
{
  const { ctx, page } = await session({ phone: true })
  await go(page, '/')
  await shot(page, 'phone-overview')
  await go(page, '/a/4')
  await shot(page, 'phone-broker')
  await ctx.close()
}
{
  const { ctx, page } = await session({ phone: true, dark: true })
  await go(page, '/plan')
  await shot(page, 'phone-plan-dark')
  await ctx.close()
}

await browser.close()
if (errors.length) {
  console.error('browser errors:\n' + errors.join('\n'))
  process.exit(1)
}
console.log('ok')
