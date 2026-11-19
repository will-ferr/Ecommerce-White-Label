import { expect, test, type Page } from '@playwright/test'

const stores = [
  {
    name: 'Aurora Cosméticos',
    other: 'Terra Café',
    url: 'http://aurora.localhost:8080/',
    canvas: 'rgb(251, 245, 247)',
    primary: 'rgb(138, 28, 92)',
    radius: '24px',
    font: 'Charter',
    headerAlignment: 'center',
  },
  {
    name: 'Terra Café',
    other: 'Aurora Cosméticos',
    url: 'http://terra.localhost:8080/',
    canvas: 'rgb(241, 238, 227)',
    primary: 'rgb(138, 75, 18)',
    radius: '0px',
    font: 'Rockwell',
    headerAlignment: 'flow-start',
  },
]

function computed(page: Page, selector: string, property: string) {
  return page.locator(selector).first().evaluate((element, name) => getComputedStyle(element).getPropertyValue(name), property)
}

for (const store of stores) {
  test(`${store.name} shows its own identity`, async ({ page }) => {
    await page.goto(store.url)

    await expect(page.getByRole('heading', { level: 1 })).toHaveText(store.name)
    await expect(page).toHaveTitle(store.name)
    await expect(page.getByText(store.other)).toHaveCount(0)

    expect(await computed(page, 'html', 'background-color')).toBe(store.canvas)
    expect(await computed(page, 'main [role="img"]', 'background-color')).toBe(store.primary)
    expect(await computed(page, 'main [role="img"]', 'border-top-left-radius')).toBe(store.radius)
    expect(await computed(page, 'h1', 'font-family')).toContain(store.font)
    expect(await computed(page, 'header > div', 'justify-content')).toBe(store.headerAlignment)
  })
}

test('an address without a store shows a clear page', async ({ page }) => {
  const response = await page.goto('http://desconhecida.localhost:8080/')

  expect(response?.status()).toBe(404)
  await expect(page.getByRole('heading', { level: 1 })).toHaveText('Loja não encontrada')
})
