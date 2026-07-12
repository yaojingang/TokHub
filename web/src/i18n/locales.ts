export const resources = {
  "zh-CN": {
    common: {
      actions: {
        refresh: "刷新",
        clear: "清空",
        clearFilters: "清空筛选",
        filter: "筛选",
        cancel: "取消",
        save: "保存",
        delete: "删除",
        edit: "编辑",
        previous: "上一页",
        next: "下一页",
        copy: "复制",
        copied: "已复制",
        logout: "退出",
        login: "登录",
        userConsole: "用户控制台",
        adminConsole: "平台管理",
        publicHome: "前台首页",
        publicBoard: "监控总览"
      },
      table: {
        loading: "正在加载...",
        emptyTitle: "暂无数据",
        emptyHint: "调整筛选条件后再试。",
        selected: "已选 {{count}} 项",
        total: "共 {{total}} 条",
        page: "第 {{page}} / {{totalPages}} 页",
        pageSize: "每页 {{pageSize}} 行",
        pagination: "分页",
        jumpTo: "跳至",
        pageNumber: "页码",
        pageSuffix: "页"
      },
      state: {
        active: "Active",
        suspended: "Suspended",
        disabled: "Disabled",
        deleted: "Deleted",
        all: "全部"
      },
      brand: {
        name: "TokHub",
        adminSubtitle: "平台管理后台",
        consoleSubtitle: "用户控制台"
      }
    },
    admin: {
      groups: {
        overview: "概览",
        resources: "平台资源",
        operations: "平台运营",
        system: "系统"
      },
      nav: {
        home: "平台总览",
        channels: "平台通道",
        gateways: "平台网关",
        members: "平台成员",
        users: "用户管理",
        orgs: "组织管理",
        openApi: "开放 API",
        recommend: "精选推荐",
        usage: "用量数据",
        alerts: "告警规则",
        audit: "日志中心",
        settings: "系统设置",
        web: "网站设置"
      },
      crumbs: {
        home: "/ 平台",
        channels: "/ 上游管理",
        gateways: "/ 平台资源 / 网关",
        members: "/ 平台资源 / 成员与密钥",
        users: "/ 平台资源 / 用户",
        orgs: "/ 平台资源 / 组织",
        openApi: "/ 对外数据接口",
        recommend: "/ 平台运营",
        usage: "/ 平台治理 / 用量",
        alerts: "/ 平台治理 / 告警",
        audit: "/ 平台治理 / 日志中心",
        settings: "/ 系统",
        web: "/ 前台首页"
      },
      users: {
        title: "用户管理",
        intro: "平台级用户治理支持新增、编辑、筛选、软删除和批量状态管理。工作区成员、专属网关 Key 和私有通道仍在用户控制台管理",
        add: "＋ 新增用户",
        stats: {
          total: "总用户",
          verified: "已验证",
          admin: "Owner/Admin",
          superVip: "Super VIP",
          deleted: "已删除"
        }
      },
      orgs: {
        title: "组织管理",
        intro: "组织管理用于平台级治理、生产清理和工作区状态审计，支持新增、编辑、筛选、软删除和批量治理",
        add: "＋ 新增组织",
        stats: {
          total: "总组织",
          system: "系统组织",
          runtime: "运行时组织",
          suspended: "暂停组织",
          deleted: "已删除"
        }
      },
      members: {
        title: "平台成员与密钥",
        intro: "统一治理平台默认组织成员和平台网关 Key。成员增删改、Key 签发、编辑和吊销都会写入审计"
      }
    },
    console: {
      groups: {
        workspace: "工作区",
        channels: "监控与通道",
        governance: "治理",
        settings: "设置"
      },
      nav: {
        home: "控制台首页",
        channels: "我的通道",
        gateways: "专属中转站",
        keys: "成员与密钥",
        usage: "用量数据",
        alerts: "告警规则",
        audit: "审计日志",
        settings: "设置中心",
        help: "帮助中心"
      },
      crumbs: {
        home: "/ 工作区",
        channels: "/ 工作区 / 通道",
        gateways: "/ 工作区 / 网关",
        keys: "/ 工作区 / 访问管理",
        members: "/ 工作区 / 访问管理",
        usage: "/ 工作区 / 用量",
        alerts: "/ 工作区 / 告警",
        audit: "/ 工作区 / 审计",
        settings: "/ 工作区 / 设置",
        help: "/ 工作区 / 帮助"
      },
      members: {
        title: "成员与密钥",
        intro: "统一管理当前工作区可使用专属中转站的成员及其 API Key。密钥由网关签发，所有调用经网关鉴权与限额，支持随时吊销而无需触碰任何上游通道凭据"
      }
    },
    public: {
      nav: {
        home: "首页",
        dashboard: "监控总览",
        pricing: "成本预估",
        recommend: "精选推荐",
        channels: "通道明细",
        providers: "供应商",
        strategy: "监控策略"
      }
    }
  },
  "en-US": {
    common: {
      actions: {
        refresh: "Refresh",
        clear: "Clear",
        clearFilters: "Clear filters",
        filter: "Filter",
        cancel: "Cancel",
        save: "Save",
        delete: "Delete",
        edit: "Edit",
        previous: "Previous",
        next: "Next",
        copy: "Copy",
        copied: "Copied",
        logout: "Log out",
        login: "Log in",
        userConsole: "User Console",
        adminConsole: "Platform Admin",
        publicHome: "Public Home",
        publicBoard: "Monitor Overview"
      },
      table: {
        loading: "Loading...",
        emptyTitle: "No data",
        emptyHint: "Adjust filters and try again.",
        selected: "{{count}} selected",
        total: "{{total}} records",
        page: "Page {{page}} / {{totalPages}}",
        pageSize: "{{pageSize}} rows per page",
        pagination: "Pagination",
        jumpTo: "Jump to",
        pageNumber: "Page number",
        pageSuffix: ""
      },
      state: {
        active: "Active",
        suspended: "Suspended",
        disabled: "Disabled",
        deleted: "Deleted",
        all: "All"
      },
      brand: {
        name: "TokHub",
        adminSubtitle: "Platform Admin",
        consoleSubtitle: "User Console"
      }
    },
    admin: {
      groups: {
        overview: "Overview",
        resources: "Resources",
        operations: "Operations",
        system: "System"
      },
      nav: {
        home: "Platform Overview",
        channels: "Platform Channels",
        gateways: "Platform Gateways",
        members: "Platform Members",
        users: "Users",
        orgs: "Organizations",
        openApi: "Open API",
        recommend: "Recommendations",
        usage: "Usage Data",
        alerts: "Alert Rules",
        audit: "Log Center",
        settings: "System Settings",
        web: "Website"
      },
      crumbs: {
        home: "/ Platform",
        channels: "/ Upstreams",
        gateways: "/ Resources / Gateways",
        members: "/ Resources / Members and Keys",
        users: "/ Resources / Users",
        orgs: "/ Resources / Organizations",
        openApi: "/ External Data API",
        recommend: "/ Operations",
        usage: "/ Governance / Usage",
        alerts: "/ Governance / Alerts",
        audit: "/ Governance / Log Center",
        settings: "/ System",
        web: "/ Public Website"
      },
      users: {
        title: "User Management",
        intro: "Manage platform users with creation, editing, filtering, soft deletion, and batch status controls. Workspace members, gateway keys, and private channels remain in the user console",
        add: "+ Add User",
        stats: {
          total: "Total Users",
          verified: "Verified",
          admin: "Owner/Admin",
          superVip: "Super VIP",
          deleted: "Deleted"
        }
      },
      orgs: {
        title: "Organization Management",
        intro: "Manage platform organizations for governance, production cleanup, and workspace status audit, including creation, editing, filtering, soft deletion, and batch actions",
        add: "+ Add Organization",
        stats: {
          total: "Total Orgs",
          system: "System Orgs",
          runtime: "Runtime Orgs",
          suspended: "Suspended",
          deleted: "Deleted"
        }
      },
      members: {
        title: "Platform Members and Keys",
        intro: "Govern default platform organization members and platform gateway keys. Member updates, key creation, edits, and revocation are audited"
      }
    },
    console: {
      groups: {
        workspace: "Workspace",
        channels: "Monitoring",
        governance: "Governance",
        settings: "Settings"
      },
      nav: {
        home: "Console Home",
        channels: "My Channels",
        gateways: "Dedicated Gateways",
        keys: "Members and Keys",
        usage: "Usage Data",
        alerts: "Alert Rules",
        audit: "Audit Logs",
        settings: "Settings Center",
        help: "Help Center"
      },
      crumbs: {
        home: "/ Workspace",
        channels: "/ Workspace / Channels",
        gateways: "/ Workspace / Gateways",
        keys: "/ Workspace / Access",
        members: "/ Workspace / Access",
        usage: "/ Workspace / Usage",
        alerts: "/ Workspace / Alerts",
        audit: "/ Workspace / Audit",
        settings: "/ Workspace / Settings",
        help: "/ Workspace / Help"
      },
      members: {
        title: "Members and Keys",
        intro: "Manage members and API keys for this workspace's dedicated gateway. Gateway-issued keys are authenticated and quota-limited, and can be revoked without touching upstream credentials"
      }
    },
    public: {
      nav: {
        home: "Home",
        dashboard: "Monitor Overview",
        pricing: "Model Pricing",
        recommend: "Recommendations",
        channels: "Channels",
        providers: "Providers",
        strategy: "Strategy"
      }
    }
  }
} as const;

export const supportedLanguages = ["zh-CN", "en-US"] as const;
export type SupportedLanguage = (typeof supportedLanguages)[number];
