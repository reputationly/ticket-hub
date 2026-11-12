import { chromium, expect } from "@playwright/test";
import { mkdir } from "node:fs/promises";
import { homedir } from "node:os";
import { join } from "node:path";

const base = process.env.BASE_URL || "http://127.0.0.1:8080";
const output =
  process.env.SCREEN_DIR || join(homedir(), "ticket-hub-validation/screens");
if (!process.env.ADMIN_PASSWORD)
  throw new Error("请通过 ADMIN_PASSWORD 提供本地演示管理员密码");
await mkdir(output, { recursive: true });
const browser = await chromium.launch({ headless: true });
const errors = [];
async function login(page) {
  page.on("pageerror", (error) => errors.push(error.message));
  await page.goto(`${base}/login`);
  await page
    .getByLabel("用户名", { exact: true })
    .fill(process.env.ADMIN_USERNAME || "admin");
  await page
    .getByLabel("密码", { exact: true })
    .fill(process.env.ADMIN_PASSWORD);
  await page.getByRole("button", { name: "登录工作空间" }).click();
  await page.waitForURL("**/tickets");
  await expect(
    page.locator(".ticket-title, .ticket-card").first(),
  ).toBeVisible();
}
async function shot(page, name) {
  await page.locator("body").press("Control+Home");
  expect(
    await page
      .locator("body")
      .evaluate((el) => el.scrollWidth <= window.innerWidth + 1),
  ).toBe(true);
  const path = join(output, `${name}.png`);
  await page.screenshot({
    path,
    fullPage: page.viewportSize().width >= 768,
    animations: "disabled",
  });
  console.log(`截图：${path}`);
}
try {
  if (!process.argv.includes("--mobile")) {
    const desktop = await browser.newPage({
      viewport: { width: 1440, height: 1000 },
      locale: "zh-CN",
    });
    await login(desktop);
    await shot(desktop, "desktop-tickets");
    await desktop.locator(".ticket-title").first().click();
    await expect(desktop.getByText("事件时间线")).toBeVisible();
    await shot(desktop, "desktop-ticket-detail");
    for (const resource of ["channels", "rules"]) {
      await desktop.goto(`${base}/admin/${resource}`);
      await expect(desktop.locator(".ant-table-row").first()).toBeVisible();
      await shot(desktop, `desktop-${resource}`);
    }
  }
  const phone = await browser.newPage({
    viewport: { width: 390, height: 844 },
    isMobile: true,
    hasTouch: true,
    locale: "zh-CN",
  });
  await login(phone);
  await expect(phone.locator(".bottom-tabs")).toBeVisible();
  await expect(phone.locator(".ant-table")).toHaveCount(0);
  await shot(phone, "mobile-tickets");
  for (const [path, name, ready] of [
    ["/messages", "mobile-messages", ".message-card"],
    ["/me", "mobile-subscription", ".form-card"],
    ["/admin", "mobile-admin", ".admin-tile"],
  ]) {
    await phone.locator(`.bottom-tabs a[href="${path}"]`).click();
    await expect(phone.locator(ready).first()).toBeVisible();
    await shot(phone, name);
  }
  await phone.locator('.admin-tile[href="/admin/channels"]').click();
  await expect(phone.locator(".management-card").first()).toBeVisible();
  await phone.getByRole("button", { name: "新建渠道" }).click();
  expect(
    await phone
      .locator(".ant-drawer-content-wrapper")
      .evaluate((el) => Math.round(el.getBoundingClientRect().width)),
  ).toBe(390);
  await phone.getByRole("button", { name: "关闭", exact: true }).click();
  await phone.locator('.bottom-tabs a[href="/messages"]').click();
  const title = `SSE 实时验证 ${Date.now()}`;
  const response = await phone.request.post(`${base}/api/dev/tickets`, {
    data: {
      kind: "fault_repair",
      upstream_id: title,
      title,
      status: "待认领",
      site: "本地验证机房",
    },
  });
  expect(response.ok()).toBe(true);
  await expect(
    phone.locator(".message-card").filter({ hasText: title }),
  ).toBeVisible();
  await phone.request.post(`${base}/api/auth/logout`);
  await phone.locator('.bottom-tabs a[href="/tickets"]').click();
  await phone.waitForURL("**/login");
  expect(errors).toEqual([]);
  console.log(
    "PASS：响应式截图、无横向溢出、移动全屏抽屉、SSE 新消息实时到达、401 自动回到登录。",
  );
} finally {
  await browser.close();
}
