from playwright.sync_api import sync_playwright
import json, urllib.request
k=open('/tmp/hq-key').read().strip()
def get(p):
    r=urllib.request.Request('http://127.0.0.1:7399'+p, headers={'X-HQ-Key':k}); return json.load(urllib.request.urlopen(r))
with sync_playwright() as p:
    b=p.chromium.launch(); pg=b.new_page(viewport={'width':1400,'height':950})
    pg.goto(f'http://127.0.0.1:7399/#k={k}'); pg.wait_for_timeout(3000)
    pg.click('#nav a[data-page=board]'); pg.wait_for_timeout(1500); pg.click('[data-view=shitbox]'); pg.wait_for_timeout(500)
    pg.locator('[data-quick=todo]').click(); pg.keyboard.type('drag me'); pg.keyboard.press('Enter'); pg.wait_for_timeout(22000)  # input still focused through polls
    el=pg.locator('#page-board .col[data-col=todo] .card', has_text='drag me').first
    n=int(el.get_attribute('data-card')); print('card', n, 'visible', el.is_visible())
    el.drag_to(pg.locator('#page-board .col[data-col=review]')); pg.wait_for_timeout(2500)
    print('after drag:', [c['column'] for c in get('/api/board') if c['number']==n])
    b.close()
