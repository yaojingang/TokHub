import { ReactNode, useCallback, useMemo, useRef, useState } from "react";
import { ConsoleShell } from "../components/ConsoleShell";

type MetricItem = {
  name: string;
  surface: string;
  definition: string;
  detail: string;
};

type HelpDiagram = {
  title: string;
  steps: Array<{ title: string; text: string; tone?: "blue" | "green" | "amber" | "red" | "gray" }>;
};

type FAQItem = {
  category: string;
  question: string;
  answer: ReactNode;
  diagram?: HelpDiagram;
};

const metricItems: MetricItem[] = [
  {
    name: "综合状态",
    surface: "前台监控总览",
    definition: "TokHub 对 L1、L2、L3 探测结果的汇总判断，通常显示为 healthy、degraded、down 或 unknown",
    detail: "它不是单一接口返回值，而是多层信号合并后的结果。比如 L1 连通正常但 L3 真实生成连续失败，综合状态会被降级，因为用户真正关心的是模型调用能不能完成。"
  },
  {
    name: "L1 基础连通",
    surface: "前台和我的通道",
    definition: "检查 endpoint 是否可连通、DNS/TLS 是否正常、基础请求是否能返回",
    detail: "L1 只证明网络层和入口可达，不能说明模型列表、鉴权、真实生成一定可用。它适合快速发现域名、证书、网关入口和网络故障。"
  },
  {
    name: "L2 模型能力",
    surface: "前台和我的通道",
    definition: "读取模型列表或能力接口，确认上游是否暴露预期模型",
    detail: "L2 用来区分入口可达但模型不可用的情况。比如 endpoint 能打开，但模型列表为空或缺少配置模型，L2 会提示能力异常。"
  },
  {
    name: "L3 真实探测",
    surface: "前台详情和我的通道",
    definition: "用极小提示词发起真实生成调用，并检查状态码、延迟、Token 和内容返回",
    detail: "L3 最接近用户真实业务调用。它会消耗少量 Token，但能发现仅靠健康检查看不到的问题，比如认证通过但生成失败、模型响应慢、返回内容为空。"
  },
  {
    name: "真实成功率",
    surface: "前台详情",
    definition: "一段时间内 L3 真实探测成功次数除以 L3 探测总次数",
    detail: "这个指标比基础可用率更严格。L1 成功但 L3 失败时，真实成功率会下降，用来判断生产调用风险。"
  },
  {
    name: "P95 延迟",
    surface: "前台详情、专属中转站",
    definition: "95% 请求可以在该时间内完成，剩余 5% 更慢",
    detail: "P95 比平均值更能反映用户体感，因为少量慢请求会被平均值稀释。交互式产品通常优先看 P95 和首 Token。"
  },
  {
    name: "首 Token",
    surface: "前台详情",
    definition: "从发起请求到收到第一个生成 Token 的时间",
    detail: "流式输出场景里，首 Token 决定用户感觉是否开始响应。总耗时不变时，首 Token 越短，交互体验越好。"
  },
  {
    name: "错误类型",
    surface: "前台详情、用量数据、审计",
    definition: "TokHub 对失败原因的分类，比如鉴权失败、模型不可用、上游不可达、限流、读取失败",
    detail: "错误类型用于定位责任边界。401/403 多半是 Key 或权限问题，429 多半是限流或额度，5xx 和 upstream_unavailable 更偏上游或网络异常。"
  },
  {
    name: "今日探测 Token 成本",
    surface: "前台监控总览",
    definition: "当天由探测任务消耗的 Token 按模型价格折算后的估算成本",
    detail: "公开平台通道由平台承担探测成本，用户私有通道使用用户自己的 Key。这个指标帮助管理员理解监控精度和成本之间的关系。"
  },
  {
    name: "我的关注",
    surface: "用户控制台首页",
    definition: "用户从前台监控总览收藏的平台通道",
    detail: "关注不会把平台通道变成用户自己的上游，只是把状态变化、延迟和异常提醒聚合到个人控制台，方便日常查看。"
  },
  {
    name: "我的通道",
    surface: "用户控制台",
    definition: "当前工作区自己添加的私有 endpoint、模型和 API Key",
    detail: "私有通道只归属当前工作区，凭据会加密保存，不会在平台后台或前台公开展示明文。"
  },
  {
    name: "今日探测 / 配额",
    surface: "我的通道",
    definition: "某个私有通道当天已执行探测次数和每日上限",
    detail: "手动 L3 探测会计入配额；连接测试用于保存前校验，不扣每日探测额度。配额用于避免用户 Key 被监控任务过度消耗，也避免异常通道反复重试。"
  },
  {
    name: "专属中转站",
    surface: "用户控制台",
    definition: "工作区自己的 OpenAI 兼容入口，统一接收成员调用并路由到健康上游",
    detail: "业务只配置一个网关地址和 TokHub 签发的 Key。上游 Key 保存在通道里，成员无需接触真实上游凭据。"
  },
  {
    name: "有效 Key",
    surface: "成员与密钥、设置中心",
    definition: "当前仍可调用专属中转站的网关 Key 数量",
    detail: "Key 可以按成员、网关、月额度和 QPS 管理。吊销 Key 只影响 TokHub 网关鉴权，不需要更换上游供应商 Key。"
  },
  {
    name: "本月请求",
    surface: "用量数据",
    definition: "当前筛选范围内经专属中转站产生的请求数",
    detail: "它来自 gateway usage event。探测任务也会进入 rollup，但可以通过来源筛选区分 gateway 和 probe。"
  },
  {
    name: "Token",
    surface: "用量数据",
    definition: "输入 Token 和输出 Token 的合计",
    detail: "如果上游返回 usage 字段，TokHub 使用真实 Token；如果上游没有返回，会按请求和响应内容进行估算，并在记录里标记为估算。"
  },
  {
    name: "成本",
    surface: "用量数据",
    definition: "Token 按模型价格表折算后的美元成本",
    detail: "成本用于预算、告警和对账参考。真实账单仍以供应商账单为准，TokHub 的价值是提前发现趋势和异常。"
  },
  {
    name: "错误率",
    surface: "用量数据",
    definition: "4xx 和 5xx 请求占总请求的比例",
    detail: "错误率用于判断网关、Key、上游和业务参数是否出现系统性问题。单次失败可以看最近请求记录，持续升高应配置告警。"
  },
  {
    name: "Daily Rollup",
    surface: "用量数据",
    definition: "按天聚合后的请求、Token、成本、错误和探测次数",
    detail: "Rollup 支撑趋势图、预算判断和告警评估。重新聚合会重跑统计，不等同于刷新页面，因此页面会明确提示风险。"
  },
  {
    name: "审计事件",
    surface: "审计日志",
    definition: "成员、通道、Key、网关、告警和设置变更留下的安全记录",
    detail: "审计用于回答谁在什么时候改了什么、结果如何、来自哪个 IP。导出会限制条数，并避免暴露敏感 metadata 原文。"
  }
];

const faqItems: FAQItem[] = [
  {
    category: "快速入门",
    question: "1. TokHub 的前台、用户控制台和平台管理后台分别负责什么？",
    answer: (
      <>
        <p>前台面向所有访问者，负责展示公开通道的健康状态、模型价格、精选推荐和监控策略。用户控制台面向登录用户和工作区成员，负责管理自己的关注、私有通道、专属中转站、成员 Key、用量、告警和审计。平台管理后台只面向 TokHub 运营方，负责平台通道、用户组织、推荐配置、Open API 授权站点和全局治理。</p>
        <p>这三个入口的权限边界不同。普通用户在用户控制台里看不到平台后台的全局资源，平台公开前台也不会显示用户的私有 Key 或工作区数据。</p>
      </>
    ),
    diagram: {
      title: "入口边界",
      steps: [
        { title: "前台", text: "公开监控、推荐、价格和详情页", tone: "blue" },
        { title: "用户控制台", text: "个人或企业工作区能力", tone: "green" },
        { title: "平台后台", text: "TokHub 运营方治理平台资源", tone: "gray" }
      ]
    }
  },
  {
    category: "快速入门",
    question: "2. 我的通道和前台公开通道有什么区别？",
    answer: (
      <>
        <p>前台公开通道由平台管理员录入和监控，所有访客都能看到聚合后的状态，但不能拿到平台的上游 Key。我的通道由用户在工作区里添加，通常是自己的 endpoint、API Key 和模型配置，只对当前工作区可见。</p>
        <p>如果只是观察生态里哪个平台通道更稳定，可以关注公开通道。如果要让自己的业务调用经过 TokHub 的监控和路由，就应添加私有通道，再把它接入专属中转站。</p>
      </>
    )
  },
  {
    category: "快速入门",
    question: "3. 为什么要先做连接测试，再让通道进入专属中转站？",
    answer: (
      <>
        <p>连接测试会检查 endpoint、鉴权、模型列表和一次最小化真实调用。只有通过测试的通道才适合放进专属中转站，否则网关可能拿到一个无法工作的上游，导致业务请求失败。</p>
        <p>如果测试失败，页面会显示失败阶段、状态码、延迟和错误类型。先修通道，再接网关，比在生产调用时才发现 401、模型不存在或上游超时更可靠。</p>
      </>
    ),
    diagram: {
      title: "私有通道接入",
      steps: [
        { title: "填写 endpoint 和 Key", text: "凭据加密保存", tone: "gray" },
        { title: "测试连接", text: "校验鉴权、模型和 L3", tone: "blue" },
        { title: "接入网关", text: "成为可路由上游", tone: "green" }
      ]
    }
  },
  {
    category: "监测原理",
    question: "4. L1、L2、L3 三层监测各自解决什么问题？",
    answer: (
      <>
        <p>L1 解决入口是否可达，重点看 DNS、TLS、基础 HTTP 和网关入口。L2 解决模型能力是否可见，重点看模型列表、目标模型是否存在和能力接口是否正常。L3 解决真实生成是否可用，重点看一次最小化模型调用能否成功、是否返回内容、延迟和 Token 是否合理。</p>
        <p>三层监测按成本和可信度递进。L1 便宜但只能证明可达，L3 成本更高但最接近真实业务。TokHub 把三层结果放在一起，是为了减少“健康检查正常但真实调用失败”的误判。</p>
      </>
    ),
    diagram: {
      title: "三层探测",
      steps: [
        { title: "L1", text: "网络入口和证书", tone: "gray" },
        { title: "L2", text: "模型列表和能力", tone: "blue" },
        { title: "L3", text: "真实生成和内容校验", tone: "green" }
      ]
    }
  },
  {
    category: "监测原理",
    question: "5. 综合状态是怎么从三层监测推导出来的？",
    answer: (
      <>
        <p>综合状态会优先尊重更接近真实调用的信号。比如 L1 正常、L2 正常，但 L3 连续失败，用户实际调用仍然不可用，所以综合状态会降级。反过来，如果 L1 短时抖动但 L3 近期稳定，状态通常不会立刻判死，而是进入 degraded 或观察态。</p>
        <p>状态判断还会结合连续失败次数、最近成功记录、错误类型和探测时间。这样可以避免一次网络抖动把通道误判为 down，也避免只看基础连通而放过真实生成故障。</p>
      </>
    ),
    diagram: {
      title: "状态合成",
      steps: [
        { title: "探测记录", text: "L1 / L2 / L3 原始结果", tone: "blue" },
        { title: "规则合成", text: "连续失败、最近成功和错误类型", tone: "amber" },
        { title: "综合状态", text: "healthy / degraded / down / unknown", tone: "green" }
      ]
    }
  },
  {
    category: "监测原理",
    question: "6. 为什么 L3 真实探测会消耗 Token？成本怎么控制？",
    answer: (
      <>
        <p>L3 会向上游发起真实模型请求，因此必然消耗少量输入和输出 Token。TokHub 会使用短提示词、低输出上限和合理频率来控制成本，同时记录 Token、估算成本和探测次数。</p>
        <p>用户私有通道会受每日探测配额限制。管理员可以从前台的探测成本、后台用量数据和通道配额判断监控频率是否合适。高价值生产通道可以提高监控频率，低频备用通道可以降低频率。</p>
      </>
    )
  },
  {
    category: "监测原理",
    question: "7. P95 延迟、平均延迟和首 Token 应该怎么看？",
    answer: (
      <>
        <p>平均延迟适合看整体水平，但很容易被少量极快或极慢请求稀释。P95 表示 95% 的请求不超过这个时间，更适合衡量真实用户体验。首 Token 只关注流式响应开始得快不快，适合聊天、IDE、Agent 这类交互式产品。</p>
        <p>如果 P95 升高但平均延迟变化不大，说明慢请求尾部变重了。若首 Token 高但总耗时正常，用户会感觉系统迟迟没有开始回应。推荐选择通道时，生产稳定性看真实成功率，交互体验看 P95 和首 Token。</p>
      </>
    )
  },
  {
    category: "监测原理",
    question: "8. 错误类型如何帮助定位问题？",
    answer: (
      <>
        <p>错误类型是排障入口。auth 相关错误通常指向 API Key、权限或签名问题；models_unavailable 通常说明模型列表不可用或目标模型不存在；upstream_unavailable 更偏网络、供应商或代理链路；429 或 quota 类错误通常和限流、余额或套餐有关。</p>
        <p>同一个错误如果只出现一次，可以先看最近探测记录。如果同类错误持续出现，应结合审计日志确认近期是否有人改过通道、Key、网关或告警规则。</p>
      </>
    )
  },
  {
    category: "专属中转站",
    question: "9. 专属中转站的调用链路是什么？",
    answer: (
      <>
        <p>业务方只调用 TokHub 的网关地址，并使用 TokHub 签发的网关 Key。网关先验证成员 Key、额度和 QPS，再根据路由策略选择健康上游，最后把请求转发给真实供应商 endpoint。上游返回后，TokHub 会记录状态码、模型、Token、成本、延迟和错误类型。</p>
        <p>这个设计的好处是业务不需要保存多个供应商 Key，也不需要自己写故障切换逻辑。通道不可用时，TokHub 可以把请求路由到其他健康上游。</p>
      </>
    ),
    diagram: {
      title: "网关调用链路",
      steps: [
        { title: "业务请求", text: "OpenAI 兼容请求", tone: "gray" },
        { title: "TokHub 网关", text: "鉴权、限额、路由和记录", tone: "blue" },
        { title: "健康上游", text: "私有通道或授权平台通道", tone: "green" },
        { title: "用量记录", text: "usage event 和 rollup", tone: "amber" }
      ]
    }
  },
  {
    category: "专属中转站",
    question: "10. 最低延迟、最高成功率、成本优先三种路由策略有什么区别？",
    answer: (
      <>
        <p>最低延迟优先会在健康上游里选择最近响应最快的通道，适合 IDE、客服和实时聊天。最高成功率会优先选择 L3 成功率最高的通道，适合生产任务和稳定性优先的业务。成本优先会在健康上游中选择成本更低的通道，适合批量、低实时性或预算敏感场景。</p>
        <p>路由策略不是固定写死的。工作区可以设置默认策略，新建专属中转站会沿用默认策略；单个网关也可以编辑策略和上游集合。</p>
      </>
    )
  },
  {
    category: "专属中转站",
    question: "11. 某个上游挂了，网关会怎么处理？",
    answer: (
      <>
        <p>当探测或真实网关调用发现上游连续失败，系统会降低它的健康评分或打开短时间熔断。后续请求会优先选择其他健康上游。如果没有可用上游，网关会返回明确错误，并在用量和审计相关记录里留下失败信息。</p>
        <p>这不是简单随机重试。TokHub 会综合状态、路由策略、近期延迟、错误类型和熔断状态，尽量避免把请求继续打到明显不健康的上游。</p>
      </>
    ),
    diagram: {
      title: "故障转移",
      steps: [
        { title: "发现失败", text: "探测或真实调用失败", tone: "red" },
        { title: "降权或熔断", text: "短时间避开故障上游", tone: "amber" },
        { title: "选择备选", text: "按策略路由到健康上游", tone: "green" }
      ]
    }
  },
  {
    category: "专属中转站",
    question: "12. 成员与密钥页面里的 Key 和上游 API Key 是一回事吗？",
    answer: (
      <>
        <p>不是。成员与密钥页面里的 Key 是 TokHub 网关 Key，用来访问当前工作区的专属中转站。上游 API Key 是你在“我的通道”里保存的供应商凭据，用来让 TokHub 代你调用真实上游。</p>
        <p>这两个 Key 的权限边界不同。网关 Key 可以随时吊销或限额，不会暴露上游凭据；上游 Key 加密保存，只在网关转发或探测时使用。</p>
      </>
    )
  },
  {
    category: "用量与成本",
    question: "13. 用量数据里的本月请求、Token 和成本从哪里来？",
    answer: (
      <>
        <p>本月请求来自网关调用和探测任务产生的 usage event。Token 优先使用上游返回的真实 usage 字段，如果上游不返回，系统会按输入和输出内容估算。成本基于模型价格表和 Token 数折算，用来做预算、告警和趋势判断。</p>
        <p>用量数据支持按天、来源、网关、通道、模型和成员筛选。想看业务调用，筛选来源为网关调用；想看监控带来的成本，筛选来源为探测任务。</p>
      </>
    )
  },
  {
    category: "用量与成本",
    question: "14. Daily Rollup 和请求记录有什么区别？",
    answer: (
      <>
        <p>请求记录是细粒度 usage event，保留单次请求的时间、网关、Key、上游、模型、状态码、Token、成本、延迟和是否估算。Daily Rollup 是按天聚合后的汇总，用来驱动趋势图、预算判断、告警评估和报表。</p>
        <p>如果你要查一笔异常调用，看请求记录。如果你要看趋势、成本走势或某个模型本月消耗，看 Daily Rollup。重新聚合只影响统计汇总，不会修改原始请求记录。</p>
      </>
    ),
    diagram: {
      title: "用量汇总",
      steps: [
        { title: "usage event", text: "单次请求记录", tone: "blue" },
        { title: "rollup job", text: "按天、来源和维度聚合", tone: "amber" },
        { title: "报表和告警", text: "趋势、预算和阈值判断", tone: "green" }
      ]
    }
  },
  {
    category: "用量与成本",
    question: "15. 成本是最终账单吗？为什么会显示估算？",
    answer: (
      <>
        <p>TokHub 的成本是按模型价格和 Token 折算的运营视图，主要用于预算预警、趋势判断和工作区内部对账。最终扣费仍以供应商账单为准，因为供应商可能有折扣、缓存、批处理或特殊计费规则。</p>
        <p>如果上游返回准确 usage，TokHub 会记录真实 Token。如果上游没有返回 usage 或返回字段不完整，系统会估算 Token，并在请求记录里标记为估算，方便对账时区分。</p>
      </>
    )
  },
  {
    category: "用量与成本",
    question: "16. 错误率升高时，应该先查哪里？",
    answer: (
      <>
        <p>先在用量数据里筛选最近时间范围，按网关、通道、模型和成员缩小范围。再查看请求记录里的状态码和错误类型，确认是某个上游、某个模型、某个 Key，还是全局网络问题。最后到审计日志里查近期是否有通道、Key、网关或设置变更。</p>
        <p>如果错误率持续升高，建议在告警规则里配置 gateway_error_rate 或 l3_consecutive_failures，避免只靠人工巡检发现问题。</p>
      </>
    )
  },
  {
    category: "告警与审计",
    question: "17. 告警规则里的阈值、窗口和去重分别是什么意思？",
    answer: (
      <>
        <p>阈值表示触发条件，比如连续失败 2 次、错误率超过某个比例、成本超过某个金额。窗口表示观察多长时间内的数据，比如 60 分钟。去重表示同类告警在多少分钟内只通知一次，避免一个故障刷屏。</p>
        <p>严肃生产通道可以设置更低阈值和更短窗口，第一时间发现问题。低价值或备用通道可以提高阈值，减少噪音。通知渠道可以选邮件、Webhook、飞书等，发送结果会保留记录。</p>
      </>
    )
  },
  {
    category: "告警与审计",
    question: "18. 审计日志记录哪些操作？用户为什么需要看它？",
    answer: (
      <>
        <p>审计日志会记录登录、成员变更、Key 签发或吊销、私有通道变更、网关配置、告警规则、通知渠道和重要设置变更。每条记录包含时间、账号、动作、对象、结果和 IP 等信息。</p>
        <p>它用于安全追溯和团队协作。比如某个网关突然失败，可以先查有没有人修改过上游集合或吊销 Key；某个通知没有发出，可以查告警规则和通知渠道是否被关闭。</p>
      </>
    )
  },
  {
    category: "工作区设置",
    question: "19. 设置中心里的工作区名称、时区和默认通知渠道会影响什么？",
    answer: (
      <>
        <p>工作区名称会显示在控制台顶栏和协作场景，不影响公开前台。时区会影响报表日期、账单周期展示和审计时间理解。默认通知渠道会作为新建告警规则或工作区级通知的默认发送目标。</p>
        <p>恢复默认只会重置工作区配置，不会删除成员、通道、网关或 Key。个人显示名称也在设置中心管理，它会同步到控制台顶部、侧栏和首页标题。</p>
      </>
    )
  },
  {
    category: "前台指标",
    question: "20. 前台推荐榜和监控总览里的指标如何配合使用？",
    answer: (
      <>
        <p>监控总览适合查事实，展示通道状态、L1/L2/L3、延迟、错误类型、供应商表现和详情页趋势。推荐榜适合做选择，把稳定、速度、价格和运营配置组合成更容易理解的入口。</p>
        <p>如果要做生产决策，不要只看推荐标题。建议先看推荐理由，再进入监控详情确认真实成功率、P95、L3 探测记录和错误分布，最后在用户控制台添加自己的私有通道或创建专属中转站。</p>
      </>
    )
  }
];

const allCategory = "全部";

export function ConsoleHelpPage() {
  const [activeCategory, setActiveCategory] = useState(allCategory);
  const categories = useMemo(() => [allCategory, ...Array.from(new Set(faqItems.map((item) => item.category)))], []);
  const filteredFAQ = activeCategory === allCategory ? faqItems : faqItems.filter((item) => item.category === activeCategory);

  return (
    <ConsoleShell title="帮助中心" crumb="/ 工作区 / 帮助">
      <div className="help-page">
        <div className="help-hero">
          <div>
            <span className="help-kicker">TokHub FAQ</span>
            <h2>用户控制台帮助中心</h2>
            <p>这里集中解释前台监控、用户后台指标、专属中转站、用量成本、告警审计和工作区设置的工作原理</p>
          </div>
          <div className="help-hero-meta" aria-label="帮助中心摘要">
            <span><b>{metricItems.length}</b> 个指标定义</span>
            <span><b>{faqItems.length}</b> 个常见问题</span>
            <span><b>L1 / L2 / L3</b> 监测原理</span>
          </div>
        </div>

        <section className="help-overview-grid" aria-label="帮助中心导览">
          <HelpSummary title="先查指标" text="遇到不懂的状态、Token、P95、Rollup 或错误率，先从指标词典确认定义和口径。" />
          <HelpSummary title="再看流程" text="专属中转站、用量聚合、告警通知这些跨模块问题，FAQ 里配了流程说明。" />
          <HelpSummary title="最后排障" text="从错误类型、请求记录、审计日志和告警记录倒推问题来源，避免只看单个页面。" />
        </section>

        <section className="card help-panel">
          <div className="set-h">
            <span>指标词典</span>
            <span className="help-count">{metricItems.length} 项</span>
          </div>
          <div className="help-metric-grid" role="list">
            {metricItems.map((item) => (
              <article className="help-metric-row" role="listitem" key={item.name}>
                <div>
                  <b>{item.name}</b>
                  <span>{item.surface}</span>
                </div>
                <p>{item.definition}</p>
                <small>{item.detail}</small>
              </article>
            ))}
          </div>
        </section>

        <section className="help-faq-section">
          <div className="section-head">
            <h2>常见问题 <span className="tag">{filteredFAQ.length}</span></h2>
            <span className="sub">按分类筛选后展开查看详细答案</span>
          </div>
          <div className="phase14-section-switch help-category-switch" role="tablist" aria-label="FAQ 分类">
            {categories.map((category) => (
              <button
                type="button"
                className={activeCategory === category ? "active" : ""}
                onClick={() => setActiveCategory(category)}
                key={category}
              >
                {category}
              </button>
            ))}
          </div>

          <div className="card help-faq-list">
            {filteredFAQ.map((item, index) => (
              <HelpFAQItem
                initiallyOpen={index < 2}
                item={item}
                key={`${activeCategory}-${item.question}`}
              />
            ))}
          </div>
        </section>
      </div>
    </ConsoleShell>
  );
}

function HelpFAQItem({ item, initiallyOpen }: { item: FAQItem; initiallyOpen: boolean }) {
  const initializedRef = useRef(false);
  const detailsRef = useCallback((node: HTMLDetailsElement | null) => {
    if (!node || initializedRef.current) {
      return;
    }
    initializedRef.current = true;
    node.open = initiallyOpen;
  }, [initiallyOpen]);

  return (
    <details className="help-faq-item" ref={detailsRef}>
      <summary>
        <span className="help-pill">{item.category}</span>
        <b>{item.question}</b>
      </summary>
      <div className="help-answer">
        {item.answer}
        {item.diagram ? <HelpFlow diagram={item.diagram} /> : null}
      </div>
    </details>
  );
}

function HelpSummary({ title, text }: { title: string; text: string }) {
  return (
    <article className="help-summary">
      <b>{title}</b>
      <p>{text}</p>
    </article>
  );
}

function HelpFlow({ diagram }: { diagram: HelpDiagram }) {
  return (
    <div className="help-flow-wrap">
      <div className="help-flow-title">{diagram.title}</div>
      <div className="help-flow">
        {diagram.steps.map((step, index) => (
          <div className="help-flow-segment" key={`${diagram.title}-${step.title}`}>
            <div className={`help-flow-step tone-${step.tone || "gray"}`}>
              <b>{step.title}</b>
              <span>{step.text}</span>
            </div>
            {index < diagram.steps.length - 1 ? <span className="help-flow-arrow" aria-hidden="true">→</span> : null}
          </div>
        ))}
      </div>
    </div>
  );
}
