from playwright.sync_api import sync_playwright
k=open('/tmp/hq-key').read().strip()
with sync_playwright() as p:
    b=p.chromium.launch()
    for name,vw in [('d',(1440,900)),('n',(1024,760)),('m',(390,844))]:
        pg=b.new_page(viewport={'width':vw[0],'height':vw[1]})
        errs=[]; pg.on('pageerror', lambda e: errs.append(str(e)))
        pg.goto(f'http://127.0.0.1:7399/#k={k}'); pg.wait_for_timeout(3000)
        for page in ['dash','games','board','changes','chat','team','settings']:
            pg.evaluate(f"show('{page}')"); pg.wait_for_timeout(1500)
            pg.screenshot(path=f'/tmp/sw-{name}-{page}.png', full_page=(name=='d'))
        if name=='d':
            pg.evaluate("show('board')"); pg.wait_for_timeout(800); pg.click('#bNew'); pg.wait_for_timeout(500); pg.screenshot(path='/tmp/sw-d-newcard.png'); pg.keyboard.press('Escape')
            pg.evaluate("show('games')"); pg.wait_for_timeout(800); pg.click('#page-games [data-game=secret] [data-op=push]'); pg.wait_for_timeout(500); pg.screenshot(path='/tmp/sw-d-push.png'); pg.keyboard.press('Escape')
            pg.click('#gEdit'); pg.wait_for_timeout(500); pg.screenshot(path='/tmp/sw-d-editgames.png'); pg.keyboard.press('Escape')
            pg.click('#jobsBtn'); pg.wait_for_timeout(300); pg.screenshot(path='/tmp/sw-d-console.png')
        print(name, 'errors', errs)
    b.close()
