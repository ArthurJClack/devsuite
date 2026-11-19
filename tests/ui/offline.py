from playwright.sync_api import sync_playwright
k=open('/tmp/hq-key').read().strip()
with sync_playwright() as p:
    b=p.chromium.launch(); pg=b.new_page(viewport={'width':1400,'height':900})
    errs=[]; pg.on('pageerror', lambda e: errs.append(str(e)))
    pg.goto(f'http://127.0.0.1:7399/#k={k}'); pg.wait_for_timeout(2500)
    pg.route('**/api/**', lambda r: r.abort())
    pg.click('#nav a[data-page=board]'); pg.wait_for_timeout(5000)
    print('banner shown:', pg.locator('#offline.on').count()==1, '| toast:', repr(pg.locator('#toast').get_attribute('class')))
    pg.unroute('**/api/**'); pg.wait_for_timeout(5000)
    print('banner gone after hq is back:', pg.locator('#offline.on').count()==0, '| board loaded:', pg.locator('#page-board .card').count()>0)
    print('errors', errs); b.close()
