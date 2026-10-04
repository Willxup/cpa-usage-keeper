// @vitest-environment happy-dom
import React, { act } from "react";
import { createRoot, type Root } from "react-dom/client";
import { afterEach, expect, it, vi } from "vitest";
import i18n from "@/i18n";
const api = vi.hoisted(() => ({ fetchAdminProviderQuota: vi.fn() }));
vi.mock("@/lib/api", async (original) => ({
  ...(await original<typeof import("@/lib/api")>()),
  ...api,
}));
import { ApiProviderQuota } from "../ApiProviderQuota";
import { AiProviderCredentialsSection } from "../AiProviderCredentialsSection";

let root: Root | undefined;
let container: HTMLDivElement;
async function mount(element: React.ReactNode) {
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  await i18n.changeLanguage("en");
  container = document.createElement("div");
  document.body.append(container);
  root = createRoot(container);
  await act(async () => {
    root!.render(element);
  });
}
afterEach(async () => {
  if (root)
    await act(async () => {
      root!.unmount();
    });
  root = undefined;
  container?.remove();
  api.fetchAdminProviderQuota.mockReset();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

it("shows only observed API providers and preserves their quota after read failure", async () => {
  vi.useFakeTimers();
  vi.spyOn(document, "hidden", "get").mockReturnValue(false);
  api.fetchAdminProviderQuota.mockResolvedValue({
    accounts: [
      {
        kind: "api",
        label: "Codex account 1",
        status: "available",
        rows: [{ label: "Weekly", remaining_percent: 75 }],
      },
      {
        kind: "oauth",
        label: "OAuth account hidden",
        status: "available",
        rows: [{ label: "Weekly", remaining_percent: 25 }],
      },
      {
        kind: "api",
        label: "Unsupported account hidden",
        status: "unavailable",
        rows: [],
      },
    ],
  });
  await mount(<ApiProviderQuota />);
  expect(container.textContent).toContain("API provider quota");
  expect(container.textContent).toContain("75%");
  expect(container.textContent).not.toContain("hidden");
  api.fetchAdminProviderQuota.mockRejectedValue(
    new Error("Synthetic unavailable"),
  );
  await act(async () => {
    await vi.advanceTimersByTimeAsync(10_000);
  });
  expect(container.querySelector('[role="alert"]')).not.toBeNull();
  expect(container.textContent).toContain("75%");
});

it("aborts and ignores a late cache response after unmount", async () => {
  let finish: (data: unknown) => void = () => {};
  api.fetchAdminProviderQuota.mockImplementation(
    () =>
      new Promise((resolve) => {
        finish = resolve;
      }),
  );
  await mount(<ApiProviderQuota />);
  const signal = api.fetchAdminProviderQuota.mock.calls[0][0] as AbortSignal;
  await act(async () => {
    root!.unmount();
  });
  root = undefined;
  expect(signal.aborted).toBe(true);
  await act(async () => {
    finish({
      accounts: [
        { kind: "api", label: "Late account", rows: [{ label: "Weekly" }] },
      ],
    });
  });
  expect(container.textContent).toBe("");
});

it("reporting-only AI Provider views never mount the administrator quota reader", async () => {
  await mount(
    <AiProviderCredentialsSection
      reportingOnly
      rows={[]}
      total={0}
      page={1}
      totalPages={1}
      pageSize={20}
      activeOnly={false}
      sort={"total_requests"}
      loading={false}
      onPageChange={() => {}}
      onPageSizeChange={() => {}}
      onActiveOnlyChange={() => {}}
      onSortChange={() => {}}
    />,
  );
  expect(api.fetchAdminProviderQuota).not.toHaveBeenCalled();
});
