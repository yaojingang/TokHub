import { ComponentType, ReactElement } from "react";
import { AdminChannelsPage } from "../pages/AdminChannelsPage";
import { AdminGatewaysPage } from "../pages/AdminGatewaysPage";
import { AdminHome } from "../pages/AdminHome";
import { AdminLoginPage } from "../pages/AdminLoginPage";
import { AdminMembersPage } from "../pages/AdminMembersPage";
import { AdminOpenAPIPage } from "../pages/AdminOpenAPIPage";
import { AdminOrgsPage } from "../pages/AdminOrgsPage";
import { AdminRecommendPage } from "../pages/AdminRecommendPage";
import { AdminRecommendNewPage } from "../pages/AdminRecommendNewPage";
import { AdminSettingsPage } from "../pages/AdminSettingsPage";
import { AdminUsagePage } from "../pages/AdminUsagePage";
import { AdminUserNewPage } from "../pages/AdminUserNewPage";
import { AdminUsersPage } from "../pages/AdminUsersPage";
import { AdminWebPage } from "../pages/AdminWebPage";
import { AlertsPage } from "../pages/AlertsPage";
import { AuditPage } from "../pages/AuditPage";
import { ChannelDetailPage } from "../pages/ChannelDetailPage";
import { ConsoleHelpPage } from "../pages/ConsoleHelpPage";
import { ConsoleSettingsPage } from "../pages/ConsoleSettingsPage";
import { DashboardFullscreenPage, DashboardPage } from "../pages/DashboardPage";
import { HomePage } from "../pages/HomePage";
import { LoginPage } from "../pages/LoginPage";
import { PricingPage } from "../pages/PricingPage";
import { PublicHome } from "../pages/PublicHome";
import { RecommendPage } from "../pages/RecommendPage";
import type { AdminSettingsSummary, WorkspaceSettings } from "../lib/api";

export type ModuleScope = "public" | "admin" | "console";
export type ModulePermission = "public" | "authenticated" | "platform-admin";

export type ModuleConfig = {
  id: string;
  scope: ModuleScope;
  path: string;
  titleKey: string;
  crumbKey?: string;
  navKey?: string;
  icon?: string;
  permission: ModulePermission;
  component: ComponentType;
  routeElement?: ReactElement;
  showInNav?: boolean;
  navGroupKey?: string;
  summaryKey?: keyof AdminSettingsSummary | keyof WorkspaceSettings;
};

function AdminGatewaysRoute() {
  return <AdminGatewaysPage scope="admin" />;
}

function AdminMembersRoute() {
  return <AdminMembersPage scope="admin" />;
}

function ConsoleGatewaysRoute() {
  return <AdminGatewaysPage />;
}

function ConsoleMembersRoute() {
  return <AdminMembersPage />;
}

function ConsoleUsageRoute() {
  return <AdminUsagePage scope="console" />;
}

function ConsoleAlertsRoute() {
  return <AlertsPage scope="console" />;
}

function ConsoleAuditRoute() {
  return <AuditPage scope="console" />;
}

function ConsoleChannelsRoute() {
  return <DashboardPage initialView="channels" />;
}

export const adminModules: ModuleConfig[] = [
  {
    id: "admin.home",
    scope: "admin",
    path: "/admin",
    titleKey: "admin:nav.home",
    crumbKey: "admin:crumbs.home",
    navKey: "admin:nav.home",
    icon: "▦",
    permission: "platform-admin",
    component: AdminHome,
    showInNav: true,
    navGroupKey: "admin:groups.overview"
  },
  {
    id: "admin.channels",
    scope: "admin",
    path: "/admin/channels",
    titleKey: "admin:nav.channels",
    crumbKey: "admin:crumbs.channels",
    navKey: "admin:nav.channels",
    icon: "⇄",
    permission: "platform-admin",
    component: AdminChannelsPage,
    showInNav: true,
    navGroupKey: "admin:groups.resources",
    summaryKey: "platformChannels"
  },
  {
    id: "admin.users",
    scope: "admin",
    path: "/admin/users",
    titleKey: "admin:users.title",
    crumbKey: "admin:crumbs.users",
    navKey: "admin:nav.users",
    icon: "⚿",
    permission: "platform-admin",
    component: AdminUsersPage,
    showInNav: true,
    navGroupKey: "admin:groups.resources",
    summaryKey: "users"
  },
  {
    id: "admin.users.new",
    scope: "admin",
    path: "/admin/users/new",
    titleKey: "admin:users.add",
    crumbKey: "admin:crumbs.users",
    permission: "platform-admin",
    component: AdminUserNewPage
  },
  {
    id: "admin.orgs",
    scope: "admin",
    path: "/admin/orgs",
    titleKey: "admin:orgs.title",
    crumbKey: "admin:crumbs.orgs",
    navKey: "admin:nav.orgs",
    icon: "◫",
    permission: "platform-admin",
    component: AdminOrgsPage,
    showInNav: true,
    navGroupKey: "admin:groups.resources",
    summaryKey: "orgs"
  },
  {
    id: "admin.open-api",
    scope: "admin",
    path: "/admin/open-api",
    titleKey: "admin:nav.openApi",
    crumbKey: "admin:crumbs.openApi",
    navKey: "admin:nav.openApi",
    icon: "◉",
    permission: "platform-admin",
    component: AdminOpenAPIPage,
    showInNav: true,
    navGroupKey: "admin:groups.resources",
    summaryKey: "openApiSites"
  },
  {
    id: "admin.recommend",
    scope: "admin",
    path: "/admin/recommend",
    titleKey: "admin:nav.recommend",
    crumbKey: "admin:crumbs.recommend",
    navKey: "admin:nav.recommend",
    icon: "★",
    permission: "platform-admin",
    component: AdminRecommendPage,
    showInNav: true,
    navGroupKey: "admin:groups.operations",
    summaryKey: "recommendPicks"
  },
  {
    id: "admin.recommend.new",
    scope: "admin",
    path: "/admin/recommend/new",
    titleKey: "admin:nav.recommend",
    crumbKey: "admin:crumbs.recommend",
    permission: "platform-admin",
    component: AdminRecommendNewPage
  },
  {
    id: "admin.usage",
    scope: "admin",
    path: "/admin/usage",
    titleKey: "admin:nav.usage",
    crumbKey: "admin:crumbs.usage",
    navKey: "admin:nav.usage",
    icon: "$",
    permission: "platform-admin",
    component: AdminUsagePage,
    showInNav: true,
    navGroupKey: "admin:groups.operations"
  },
  {
    id: "admin.alerts",
    scope: "admin",
    path: "/admin/alerts",
    titleKey: "admin:nav.alerts",
    crumbKey: "admin:crumbs.alerts",
    navKey: "admin:nav.alerts",
    icon: "⚑",
    permission: "platform-admin",
    component: AlertsPage,
    showInNav: true,
    navGroupKey: "admin:groups.operations",
    summaryKey: "enabledAdminAlertRules"
  },
  {
    id: "admin.audit",
    scope: "admin",
    path: "/admin/audit",
    titleKey: "admin:nav.audit",
    crumbKey: "admin:crumbs.audit",
    navKey: "admin:nav.audit",
    icon: "≡",
    permission: "platform-admin",
    component: AuditPage,
    showInNav: true,
    navGroupKey: "admin:groups.operations",
    summaryKey: "auditToday"
  },
  {
    id: "admin.settings",
    scope: "admin",
    path: "/admin/settings",
    titleKey: "admin:nav.settings",
    crumbKey: "admin:crumbs.settings",
    navKey: "admin:nav.settings",
    icon: "⚙",
    permission: "platform-admin",
    component: AdminSettingsPage,
    showInNav: true,
    navGroupKey: "admin:groups.system"
  },
  {
    id: "admin.web",
    scope: "admin",
    path: "/admin/web",
    titleKey: "admin:nav.web",
    crumbKey: "admin:crumbs.web",
    navKey: "admin:nav.web",
    icon: "⌂",
    permission: "platform-admin",
    component: AdminWebPage,
    showInNav: true,
    navGroupKey: "admin:groups.system"
  },
  {
    id: "admin.gateways",
    scope: "admin",
    path: "/admin/gateways",
    titleKey: "admin:nav.gateways",
    crumbKey: "admin:crumbs.gateways",
    navKey: "admin:nav.gateways",
    icon: "✦",
    permission: "platform-admin",
    component: AdminGatewaysRoute,
    showInNav: true,
    navGroupKey: "admin:groups.resources",
    summaryKey: "gateways"
  },
  {
    id: "admin.members",
    scope: "admin",
    path: "/admin/members",
    titleKey: "admin:members.title",
    crumbKey: "admin:crumbs.members",
    navKey: "admin:nav.members",
    icon: "⚿",
    permission: "platform-admin",
    component: AdminMembersRoute,
    showInNav: true,
    navGroupKey: "admin:groups.resources",
    summaryKey: "activeGatewayKeys"
  }
];

export const consoleModules: ModuleConfig[] = [
  {
    id: "console.home",
    scope: "console",
    path: "/console",
    titleKey: "console:nav.home",
    crumbKey: "console:crumbs.home",
    navKey: "console:nav.home",
    icon: "▦",
    permission: "authenticated",
    component: DashboardPage,
    showInNav: true,
    navGroupKey: "console:groups.workspace"
  },
  {
    id: "console.fullscreen",
    scope: "console",
    path: "/console/fullscreen",
    titleKey: "console:nav.home",
    crumbKey: "console:crumbs.home",
    navKey: "console:nav.home",
    icon: "▣",
    permission: "authenticated",
    component: DashboardFullscreenPage
  },
  {
    id: "console.channels",
    scope: "console",
    path: "/console/channels",
    titleKey: "console:nav.channels",
    crumbKey: "console:crumbs.channels",
    navKey: "console:nav.channels",
    icon: "⇄",
    permission: "authenticated",
    component: ConsoleChannelsRoute,
    showInNav: true,
    navGroupKey: "console:groups.channels",
    summaryKey: "privateChannels"
  },
  {
    id: "console.gateways",
    scope: "console",
    path: "/console/gateways",
    titleKey: "console:nav.gateways",
    crumbKey: "console:crumbs.gateways",
    navKey: "console:nav.gateways",
    icon: "✦",
    permission: "authenticated",
    component: ConsoleGatewaysRoute,
    showInNav: true,
    navGroupKey: "console:groups.channels",
    summaryKey: "gateways"
  },
  {
    id: "console.keys",
    scope: "console",
    path: "/console/keys",
    titleKey: "console:members.title",
    crumbKey: "console:crumbs.keys",
    navKey: "console:nav.keys",
    icon: "⚿",
    permission: "authenticated",
    component: ConsoleMembersRoute,
    showInNav: true,
    navGroupKey: "console:groups.channels",
    summaryKey: "activeKeys"
  },
  {
    id: "console.members",
    scope: "console",
    path: "/console/members",
    titleKey: "console:members.title",
    crumbKey: "console:crumbs.members",
    navKey: "console:nav.keys",
    icon: "⚿",
    permission: "authenticated",
    component: ConsoleMembersRoute
  },
  {
    id: "console.usage",
    scope: "console",
    path: "/console/usage",
    titleKey: "console:nav.usage",
    crumbKey: "console:crumbs.usage",
    navKey: "console:nav.usage",
    icon: "$",
    permission: "authenticated",
    component: ConsoleUsageRoute,
    showInNav: true,
    navGroupKey: "console:groups.governance"
  },
  {
    id: "console.alerts",
    scope: "console",
    path: "/console/alerts",
    titleKey: "console:nav.alerts",
    crumbKey: "console:crumbs.alerts",
    navKey: "console:nav.alerts",
    icon: "⚑",
    permission: "authenticated",
    component: ConsoleAlertsRoute,
    showInNav: true,
    navGroupKey: "console:groups.governance"
  },
  {
    id: "console.audit",
    scope: "console",
    path: "/console/audit",
    titleKey: "console:nav.audit",
    crumbKey: "console:crumbs.audit",
    navKey: "console:nav.audit",
    icon: "≡",
    permission: "authenticated",
    component: ConsoleAuditRoute,
    showInNav: true,
    navGroupKey: "console:groups.governance"
  },
  {
    id: "console.settings",
    scope: "console",
    path: "/console/settings",
    titleKey: "console:nav.settings",
    crumbKey: "console:crumbs.settings",
    navKey: "console:nav.settings",
    icon: "⚙",
    permission: "authenticated",
    component: ConsoleSettingsPage,
    showInNav: true,
    navGroupKey: "console:groups.settings"
  },
  {
    id: "console.help",
    scope: "console",
    path: "/console/help",
    titleKey: "console:nav.help",
    crumbKey: "console:crumbs.help",
    navKey: "console:nav.help",
    icon: "?",
    permission: "authenticated",
    component: ConsoleHelpPage,
    showInNav: true,
    navGroupKey: "console:groups.settings"
  }
];

export const publicModules: ModuleConfig[] = [
  { id: "public.home", scope: "public", path: "/", titleKey: "public:nav.home", navKey: "public:nav.home", permission: "public", component: HomePage },
  { id: "public.dashboard", scope: "public", path: "/dashboard", titleKey: "public:nav.dashboard", navKey: "public:nav.dashboard", permission: "public", component: PublicHome },
  { id: "public.login", scope: "public", path: "/login", titleKey: "common:actions.login", permission: "public", component: LoginPage },
  { id: "admin.login", scope: "public", path: "/admin/login", titleKey: "common:actions.login", permission: "public", component: AdminLoginPage },
  { id: "public.channelDetail", scope: "public", path: "/channels/:channelID", titleKey: "public:nav.channels", permission: "public", component: ChannelDetailPage },
  { id: "public.pricing", scope: "public", path: "/pricing", titleKey: "public:nav.pricing", navKey: "public:nav.pricing", permission: "public", component: PricingPage },
  { id: "public.recommend", scope: "public", path: "/recommend", titleKey: "public:nav.recommend", navKey: "public:nav.recommend", permission: "public", component: RecommendPage }
];

export const modules: ModuleConfig[] = [...publicModules, ...adminModules, ...consoleModules];

export function moduleElement(module: ModuleConfig) {
  if (module.routeElement) return module.routeElement;
  const Component = module.component;
  return <Component />;
}

export function groupedNavModules(scope: "admin" | "console") {
  const source = scope === "admin" ? adminModules : consoleModules;
  const groups: Array<{ groupKey: string; modules: ModuleConfig[] }> = [];
  for (const module of source) {
    if (!module.showInNav || !module.navGroupKey) continue;
    let group = groups.find((item) => item.groupKey === module.navGroupKey);
    if (!group) {
      group = { groupKey: module.navGroupKey, modules: [] };
      groups.push(group);
    }
    group.modules.push(module);
  }
  return groups;
}
