// Smoke coverage for the v3 fog-map canvas UI: root display -> add a node
// via the API directly (mirroring what a WebMCP add_nodes tool call would
// do) -> drag the human-created root -> select + toggle fog on the added
// node. Locators are scoped to visible elements (data-testid attributes on
// the actual UI), per project convention. The canvas's own node-creation
// UI uses window.prompt(), so this scenario auto-accepts prompts by message
// text rather than driving them for the human-placement path — the API
// call below stands in for "the agent added a node".
export default async ({
  page,
  mark,
  baseUrl,
}: {
  page: import('playwright').Page;
  mark: (label: string) => Promise<void> | void;
  baseUrl: string;
}) => {
  page.on('dialog', (d) => d.accept('住宅購入'));

  await page.goto(baseUrl);
  await page.waitForURL(/\/c\//);
  await page.dblclick('.canvas', { position: { x: 300, y: 300 } });
  await page.waitForSelector('[data-testid="map-node"]');
  await mark('root-placed');

  const rootId = await page.getAttribute('[data-testid="map-node"]', 'data-node-id');

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
  await page.waitForFunction(() => document.querySelectorAll('[data-testid="map-node"]').length === 2);
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

  // Select the child node and clear/toggle its fog via the floating toolbar.
  const childHandle = page.locator('[data-testid="map-node"]').filter({ hasNotText: '住宅購入' });
  await childHandle.click();
  await page.waitForSelector('[data-testid="node-toggle-fog"]');
  await page.click('[data-testid="node-toggle-fog"]');
  await page.waitForSelector('.node.fogged');
  await mark('node-fogged');
};
