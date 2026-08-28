// Smoke coverage for the v3 fog-map canvas UI: root placement via inline
// editing (dblclick -> type -> Enter) -> a plain click on the root sprouts
// a child and drops straight into its inline editing (the click-to-grow
// operation) -> add a second child via the API directly (mirroring a
// WebMCP add_nodes tool call) -> drag the root -> right-click (contextmenu)
// the click-grown child to open its floating toolbar and toggle fog.
// Locators are scoped to visible elements (data-testid attributes on the
// actual UI), per project convention.
export default async ({
  page,
  mark,
  baseUrl,
}: {
  page: import('playwright').Page;
  mark: (label: string) => Promise<void> | void;
  baseUrl: string;
}) => {
  await page.goto(baseUrl);
  await page.waitForURL(/\/c\//);
  await page.dblclick('.canvas', { position: { x: 300, y: 300 } });
  await page.waitForSelector('[data-testid="node-edit-input"]');
  await page.keyboard.type('住宅購入');
  await page.keyboard.press('Enter');
  await page.waitForSelector('[data-testid="map-node"]:not(.editing)');
  await mark('root-placed');

  const rootId = await page.getAttribute('[data-testid="map-node"]', 'data-node-id');

  // A plain click on the root must sprout a child and drop straight into
  // editing it — no separate "add" affordance.
  await page.locator(`[data-node-id="${rootId}"]`).click();
  await page.waitForSelector('[data-testid="node-edit-input"]');
  await page.keyboard.type('住宅ローンの基本を知る');
  await page.keyboard.press('Enter');
  await page.waitForSelector('[data-testid="map-node"]:not(.editing)');
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="map-node"]').length === 2);
  await mark('click-grew-child');

  const clickChildId = await page.$$eval(
    '[data-testid="map-node"]',
    (els, rootId) => els.map((e) => e.getAttribute('data-node-id')).find((id) => id !== rootId),
    rootId,
  );

  const map = await page.evaluate(async () => {
    const id = window.location.pathname.split('/c/')[1];
    const res = await fetch(`/api/canvas/${id}`);
    return res.json();
  });

  await page.evaluate(
    async ({ readToken, parent }: { readToken: string; parent: string }) => {
      const id = window.location.pathname.split('/c/')[1];
      await fetch(`/api/canvas/${id}/nodes`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ readToken, parent, nodes: [{ text: '予算の全体像をつかむ' }] }),
      });
    },
    { readToken: map.readToken as string, parent: rootId! },
  );

  await page.reload();
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="map-node"]').length === 3);
  await mark('node-added');

  // Drag the root node a bit.
  const rootHandle = page.locator(`[data-node-id="${rootId}"]`);
  const box = await rootHandle.boundingBox();
  if (!box) throw new Error('root node has no bounding box');
  await page.mouse.move(box.x + box.width / 2, box.y + box.height / 2);
  await page.mouse.down();
  await page.mouse.move(box.x + box.width / 2 + 80, box.y + box.height / 2 + 40, { steps: 8 });
  await page.mouse.up();
  await mark('node-dragged');

  // Right-click (contextmenu) the click-grown child to open its toolbar and toggle fog.
  await page.locator(`[data-node-id="${clickChildId}"]`).click({ button: 'right' });
  await page.waitForSelector('[data-testid="node-toggle-fog"]');
  await page.click('[data-testid="node-toggle-fog"]');
  await page.waitForSelector('.node.fogged');
  await mark('node-fogged');
};
