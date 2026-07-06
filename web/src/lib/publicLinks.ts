import type { NavItem } from "./api";

export const defaultPrimaryPublicLinks: NavItem[] = [
  { label: "首页", href: "/" },
  { label: "监控总览", href: "/dashboard" },
  { label: "成本预估", href: "/pricing" },
  { label: "精选推荐", href: "/recommend" }
];

export const defaultFooterPublicLinks: NavItem[] = [
  ...defaultPrimaryPublicLinks,
  { label: "控制台", href: "/console" },
  { label: "平台管理", href: "/admin" }
];

export function normalizeLegacyPublicLinks(items: NavItem[]) {
  const out = items.map((item) => ({ label: item.label.trim(), href: item.href.trim() }));
  let homeIndex = -1;
  let dashboardIndex = -1;
  let recommendIndex = -1;
  let legacyMonitorHome = false;
  let hasDashboard = false;
  let hasPricing = false;

  out.forEach((item, index) => {
    if (item.href === "/") {
      if (homeIndex < 0) homeIndex = index;
      if (isLegacyMonitorHomeLabel(item.label)) {
        legacyMonitorHome = true;
        item.label = "首页";
      }
    }
    if (item.href === "/dashboard") {
      dashboardIndex = index;
      hasDashboard = true;
      if (!item.label || item.label.includes("首页")) item.label = "监控总览";
    }
    if (item.href === "/pricing") {
      hasPricing = true;
      if (!item.label || item.label.includes("价格") || item.label.includes("定价")) item.label = "成本预估";
    }
    if (item.href === "/recommend") {
      recommendIndex = index;
    }
  });

  if (!hasDashboard && homeIndex >= 0 && legacyMonitorHome) {
    out.splice(homeIndex + 1, 0, { label: "监控总览", href: "/dashboard" });
    dashboardIndex = homeIndex + 1;
    if (recommendIndex > homeIndex) recommendIndex += 1;
  }

  if (!hasPricing) {
    const pricingItem = { label: "成本预估", href: "/pricing" };
    if (dashboardIndex >= 0) {
      out.splice(dashboardIndex + 1, 0, pricingItem);
    } else if (recommendIndex >= 0) {
      out.splice(recommendIndex, 0, pricingItem);
    } else if (homeIndex >= 0) {
      out.splice(homeIndex + 1, 0, pricingItem);
    } else {
      out.push(pricingItem);
    }
  }

  return out;
}

function isLegacyMonitorHomeLabel(label: string) {
  const lower = label.toLowerCase();
  return label.includes("监控") || label.includes("总览") || lower.includes("monitor") || lower.includes("dashboard");
}
