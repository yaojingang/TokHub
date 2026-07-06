export type PriceCategory = "frontier" | "balanced" | "economy" | "reasoning" | "coding" | "embedding";

export type ModelPrice = {
  provider: string;
  family: string;
  model: string;
  modelId: string;
  category: PriceCategory;
  context: string;
  maxOutput: string;
  input: number;
  output: number;
  cacheRead?: number;
  batch: string;
  note: string;
  source: string;
  checkedAt: string;
};

export type PricingScenario = {
  id: string;
  name: string;
  description: string;
  dailyRequests: number;
  inputTokens: number;
  outputTokens: number;
  cacheHitRate: number;
};

export const priceRows: ModelPrice[] = [
  {
    provider: "OpenAI",
    family: "GPT-4.1",
    model: "GPT-4.1",
    modelId: "gpt-4.1",
    category: "frontier",
    context: "1M",
    maxOutput: "32K",
    input: 2,
    output: 8,
    cacheRead: 0.5,
    batch: "Batch API 50%",
    note: "通用旗舰，适合复杂 Agent 和多步推理。",
    source: "https://openai.com/api/pricing/",
    checkedAt: "2026-06-26"
  },
  {
    provider: "OpenAI",
    family: "GPT-4.1 mini",
    model: "GPT-4.1 mini",
    modelId: "gpt-4.1-mini",
    category: "balanced",
    context: "1M",
    maxOutput: "32K",
    input: 0.4,
    output: 1.6,
    cacheRead: 0.1,
    batch: "Batch API 50%",
    note: "均衡成本和质量，适合生产默认模型。",
    source: "https://openai.com/api/pricing/",
    checkedAt: "2026-06-26"
  },
  {
    provider: "OpenAI",
    family: "GPT-4.1 nano",
    model: "GPT-4.1 nano",
    modelId: "gpt-4.1-nano",
    category: "economy",
    context: "1M",
    maxOutput: "32K",
    input: 0.1,
    output: 0.4,
    cacheRead: 0.025,
    batch: "Batch API 50%",
    note: "高频分类、路由、抽取任务的低价选择。",
    source: "https://openai.com/api/pricing/",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Anthropic",
    family: "Claude 5",
    model: "Claude Fable 5",
    modelId: "claude-fable-5",
    category: "frontier",
    context: "200K",
    maxOutput: "64K",
    input: 10,
    output: 50,
    cacheRead: 1,
    batch: "Batch 50%",
    note: "官方文档列为 Claude 5 系列，适合高价值复杂任务。",
    source: "https://platform.claude.com/docs/en/about-claude/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Anthropic",
    family: "Claude 4",
    model: "Claude Sonnet 4.5",
    modelId: "claude-sonnet-4.5",
    category: "balanced",
    context: "200K",
    maxOutput: "64K",
    input: 3,
    output: 15,
    cacheRead: 0.3,
    batch: "Batch 50%",
    note: "代码、写作和 Agent 工作流常用基准模型。",
    source: "https://platform.claude.com/docs/en/about-claude/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Anthropic",
    family: "Claude 4",
    model: "Claude Haiku 4.5",
    modelId: "claude-haiku-4.5",
    category: "economy",
    context: "200K",
    maxOutput: "32K",
    input: 1,
    output: 5,
    cacheRead: 0.1,
    batch: "Batch 50%",
    note: "低延迟和中低成本任务的 Claude 入口。",
    source: "https://platform.claude.com/docs/en/about-claude/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Google",
    family: "Gemini 3",
    model: "Gemini 3.5 Flash",
    modelId: "gemini-3.5-flash",
    category: "balanced",
    context: "1M",
    maxOutput: "64K",
    input: 1.5,
    output: 9,
    cacheRead: 0.15,
    batch: "无公开 Batch 折扣",
    note: "Google 官方定位为高智能与速度结合，输出价需重点关注。",
    source: "https://ai.google.dev/gemini-api/docs/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Google",
    family: "Gemini 3",
    model: "Gemini 3.1 Pro Preview",
    modelId: "gemini-3.1-pro-preview",
    category: "frontier",
    context: "1M",
    maxOutput: "64K",
    input: 2,
    output: 12,
    cacheRead: 0.2,
    batch: "提示词 >200K 另有高阶价",
    note: "表内采用 <=200K prompts 价格，长上下文会更贵。",
    source: "https://ai.google.dev/gemini-api/docs/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Google",
    family: "Gemini 3",
    model: "Gemini 3.1 Flash-Lite",
    modelId: "gemini-3.1-flash-lite",
    category: "economy",
    context: "1M",
    maxOutput: "64K",
    input: 0.25,
    output: 1.5,
    cacheRead: 0.025,
    batch: "无公开 Batch 折扣",
    note: "高频 Agent 子任务、翻译和简单数据处理。",
    source: "https://ai.google.dev/gemini-api/docs/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "xAI",
    family: "Grok",
    model: "Grok 4.3",
    modelId: "grok-4.3",
    category: "frontier",
    context: "256K",
    maxOutput: "32K",
    input: 1.25,
    output: 2.5,
    batch: "无公开 Batch 折扣",
    note: "官方模型页列出每 1M token 输入和输出价格。",
    source: "https://docs.x.ai/developers/models",
    checkedAt: "2026-06-26"
  },
  {
    provider: "xAI",
    family: "Grok Code",
    model: "Grok Build 0.1",
    modelId: "grok-build-0.1",
    category: "coding",
    context: "256K",
    maxOutput: "32K",
    input: 1,
    output: 2,
    batch: "无公开 Batch 折扣",
    note: "xAI 面向构建和代码工作流的模型线。",
    source: "https://docs.x.ai/developers/models",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Mistral",
    family: "Mistral Large",
    model: "Mistral Large",
    modelId: "mistral-large",
    category: "frontier",
    context: "128K",
    maxOutput: "32K",
    input: 2,
    output: 6,
    batch: "Batch 50%",
    note: "官网 FAQ 明确列出 $2/M in 和 $6/M out。",
    source: "https://mistral.ai/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Mistral",
    family: "Codestral",
    model: "Codestral",
    modelId: "codestral",
    category: "coding",
    context: "256K",
    maxOutput: "32K",
    input: 0.3,
    output: 0.9,
    batch: "Batch 50%",
    note: "代码生成和补全场景，具体版本以 La Plateforme 为准。",
    source: "https://mistral.ai/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "DeepSeek",
    family: "DeepSeek",
    model: "DeepSeek Chat",
    modelId: "deepseek-chat",
    category: "economy",
    context: "64K",
    maxOutput: "8K",
    input: 0.27,
    output: 1.1,
    cacheRead: 0.07,
    batch: "无公开 Batch 折扣",
    note: "官方 USD 表区分 cache hit 和 cache miss，input 使用 cache miss。",
    source: "https://api-docs.deepseek.com/quick_start/pricing-details-usd",
    checkedAt: "2026-06-26"
  },
  {
    provider: "DeepSeek",
    family: "DeepSeek",
    model: "DeepSeek Reasoner",
    modelId: "deepseek-reasoner",
    category: "reasoning",
    context: "64K",
    maxOutput: "8K",
    input: 0.55,
    output: 2.19,
    cacheRead: 0.14,
    batch: "无公开 Batch 折扣",
    note: "包含 CoT 预算，适合推理任务，输出价按官方表。",
    source: "https://api-docs.deepseek.com/quick_start/pricing-details-usd",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Cohere",
    family: "Command",
    model: "Command A",
    modelId: "command-a",
    category: "balanced",
    context: "256K",
    maxOutput: "8K",
    input: 1,
    output: 2,
    batch: "无公开 Batch 折扣",
    note: "企业场景通用模型，官网列出 $1/M input 和 $2/M output。",
    source: "https://cohere.com/pricing",
    checkedAt: "2026-06-26"
  },
  {
    provider: "Cohere",
    family: "Command",
    model: "Command R7B",
    modelId: "command-r7b",
    category: "economy",
    context: "128K",
    maxOutput: "8K",
    input: 0.3,
    output: 0.6,
    batch: "无公开 Batch 折扣",
    note: "轻量 RAG 和工具调用任务的低价模型。",
    source: "https://cohere.com/pricing",
    checkedAt: "2026-06-26"
  }
];

export const pricingScenarios: PricingScenario[] = [
  {
    id: "agent",
    name: "代码 Agent",
    description: "多轮上下文、工具调用和中等输出，适合 IDE / CLI 代理。",
    dailyRequests: 1200,
    inputTokens: 4500,
    outputTokens: 1200,
    cacheHitRate: 35
  },
  {
    id: "rag",
    name: "RAG 问答",
    description: "检索增强问答，输入较长，输出稳定。",
    dailyRequests: 3000,
    inputTokens: 2800,
    outputTokens: 650,
    cacheHitRate: 20
  },
  {
    id: "batch",
    name: "批量分类",
    description: "高频短输入短输出，重点看输入价和吞吐。",
    dailyRequests: 50000,
    inputTokens: 500,
    outputTokens: 80,
    cacheHitRate: 5
  },
  {
    id: "summary",
    name: "长文总结",
    description: "长输入、中等输出，缓存和长上下文价格影响明显。",
    dailyRequests: 600,
    inputTokens: 18000,
    outputTokens: 1800,
    cacheHitRate: 45
  }
];

export const categoryLabels: Record<PriceCategory, string> = {
  frontier: "旗舰",
  balanced: "均衡",
  economy: "低价",
  reasoning: "推理",
  coding: "代码",
  embedding: "向量"
};
