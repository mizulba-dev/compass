// Smoke coverage for the Compass canvas UI: create -> display -> add a task
// via the API directly (mirroring what a WebMCP tool call would do) ->
// complete it -> delete it. Locators are scoped to visible elements
// (data-testid attributes on the actual UI), per project convention.
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
  await page.waitForSelector('[data-testid="live-status"]');
  await mark('canvas-created');

  const canvas = await page.evaluate(async () => {
    const id = window.location.pathname.split('/c/')[1];
    const res = await fetch(`/api/canvas/${id}`);
    return res.json();
  });

  await page.evaluate(
    async ({ readToken }: { readToken: string }) => {
      const id = window.location.pathname.split('/c/')[1];
      await fetch(`/api/canvas/${id}/tasks/plan`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ readToken, tasks: [{ text: 'Write outline' }] }),
      });
    },
    { readToken: canvas.readToken as string },
  );

  await page.reload();
  await page.waitForSelector('[data-testid="task-item"]');
  await mark('task-added');

  await page.click('[data-testid="task-toggle"]');
  await page.waitForSelector('.task-row.done');
  await mark('task-completed');

  await page.click('[data-testid="task-delete"]');
  await page.waitForSelector('.empty-hint');
  await mark('task-deleted');
};
