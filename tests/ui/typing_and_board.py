from playwright.sync_api import sync_playwright
import json, urllib.request
k=open('/tmp/hq-key').read().strip()
def get(p):
    r=urllib.request.Request('http://127.0.0.1:7399'+p, headers={'X-HQ-Key':k}); return json.load(urllib.request.urlopen(r))
with sync_playwright() as p:
    b=p.chromium.launch(); pg=b.new_page(viewport={'width':1400,'height':950})
    errs=[]; pg.on('pageerror', lambda e: errs.append(str(e)))
    pg.goto(f'http://127.0.0.1:7399/#k={k}'); pg.wait_for_timeout(3000)
    pg.click('#nav a[data-page=dash]'); pg.wait_for_timeout(1500)
    inp=pg.locator('#page-dash .sayIn'); inp.click(); pg.keyboard.type('hello from dash', delay=300); pg.wait_for_timeout(6000)
    print('dash typed:', repr(pg.locator('#page-dash .sayIn').input_value()))
    pg.locator('#page-dash .sayGo').click(); pg.wait_for_timeout(2500)
    print('dash send button works:', any('hello from dash' in m['text'] for m in get('/api/chat')))
    pg.click('#nav a[data-page=chat]'); pg.wait_for_timeout(1000)
    pg.locator('#page-chat .sayIn').click(); pg.keyboard.type('typed on chat page'); pg.keyboard.press('Enter'); pg.wait_for_timeout(2500)
    print('chat page enter works:', any('typed on chat page' in m['text'] for m in get('/api/chat')))
    pg.click('#nav a[data-page=board]'); pg.wait_for_timeout(1500)
    pg.click('[data-view=all]'); pg.wait_for_timeout(300)
    q=pg.locator('[data-quick=todo]'); q.click(); pg.keyboard.type('quick card'); pg.keyboard.press('Enter'); pg.wait_for_timeout(2000)
    print('quick add:', any(c['title']=='quick card' for c in get('/api/board')))
    pg.wait_for_timeout(16000)  # let polls happen
    n=[c['number'] for c in get('/api/board') if c['title']=='quick card'][0]
    card=pg.locator(f'#page-board [data-card="{n}"]')
    card.drag_to(pg.locator('#page-board .col[data-col=doing]')); pg.wait_for_timeout(2500)
    print('drag to doing:', [c['column'] for c in get('/api/board') if c['number']==n])
    pg.locator(f'#page-board [data-card="{n}"]').click(); pg.wait_for_timeout(1200)
    pg.locator('#csay').click(); pg.keyboard.type('comment typed'); pg.keyboard.press('Enter'); pg.wait_for_timeout(2000)
    print('card comment:', any('comment typed' in m['text'] for m in get(f'/api/board/{n}/comments')))
    pg.locator('#ct').click(); pg.keyboard.press('End'); pg.keyboard.type(' edited'); pg.locator('#cadd').click(); pg.keyboard.type('step one'); pg.keyboard.press('Enter'); pg.click('#cs'); pg.wait_for_timeout(2000)
    c=[c for c in get('/api/board') if c['number']==n][0]; print('modal edit + checklist:', c['title'], c['checkTotal'])
    print('errors', errs); b.close()
