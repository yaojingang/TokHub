alter table channels
  add column if not exists provider_config jsonb not null default '{}'::jsonb;

create index if not exists idx_channels_provider_config_gin on channels using gin(provider_config);

with model_seed(id, provider, model_key, display_name, context_window, input_per_mtok, output_per_mtok) as (
  values
    ('mdl_gpt54','OpenAI','gpt-5.4','GPT-5.4',1000000,2.5000,15.0000),
    ('mdl_gpt54mini','OpenAI','gpt-5.4-mini','GPT-5.4 mini',1000000,0.7500,4.5000),
    ('mdl_gpt54nano','OpenAI','gpt-5.4-nano','GPT-5.4 nano',1000000,0.2000,1.2500),
    ('mdl_gpt41','OpenAI','gpt-4.1','GPT-4.1',1000000,2.0000,8.0000),
    ('mdl_gpt41mini','OpenAI','gpt-4.1-mini','GPT-4.1 mini',1000000,0.4000,1.6000),
    ('mdl_gpt41nano','OpenAI','gpt-4.1-nano','GPT-4.1 nano',1000000,0.1000,0.4000),
    ('mdl_gpt4o','OpenAI','gpt-4o','GPT-4o',128000,2.5000,10.0000),
    ('mdl_gpt4omini','OpenAI','gpt-4o-mini','GPT-4o mini',128000,0.1500,0.6000),
    ('mdl_claude46sonnet','Anthropic','claude-sonnet-4-6','Claude Sonnet 4.6',200000,3.0000,15.0000),
    ('mdl_claude45haiku','Anthropic','claude-haiku-4-5','Claude Haiku 4.5',200000,1.0000,5.0000),
    ('mdl_claude45opus','Anthropic','claude-opus-4-5','Claude Opus 4.5',200000,5.0000,25.0000),
    ('mdl_claude35','Anthropic','claude-3-5-sonnet','Claude 3.5 Sonnet',200000,3.0000,15.0000),
    ('mdl_gemini25pro','Google','gemini-2.5-pro','Gemini 2.5 Pro',1000000,1.2500,10.0000),
    ('mdl_gemini20flash','Google','gemini-2.0-flash','Gemini 2.0 Flash',1000000,0.1000,0.4000),
    ('mdl_gemini15','Google','gemini-1.5-pro','Gemini 1.5 Pro',1000000,1.2500,10.0000),
    ('mdl_deepseekv4flash','DeepSeek','deepseek-v4-flash','DeepSeek V4 Flash',1000000,0.1400,0.2800),
    ('mdl_deepseekv4pro','DeepSeek','deepseek-v4-pro','DeepSeek V4 Pro',1000000,0.4350,0.8700),
    ('mdl_deepseekchat','DeepSeek','deepseek-chat','DeepSeek Chat',1000000,0.1400,0.2800),
    ('mdl_deepseekreasoner','DeepSeek','deepseek-reasoner','DeepSeek Reasoner',1000000,0.4350,0.8700),
    ('mdl_mistrallarge','Mistral','mistral-large-latest','Mistral Large',128000,2.0000,6.0000),
    ('mdl_mistralsmall','Mistral','mistral-small-latest','Mistral Small',128000,0.2000,0.6000),
    ('mdl_groqlama33','Groq','llama-3.3-70b-versatile','Llama 3.3 70B Versatile',128000,0.5900,0.7900),
    ('mdl_groqllama31','Groq','llama-3.1-8b-instant','Llama 3.1 8B Instant',128000,0.0500,0.0800),
    ('mdl_groqqwen332','Groq','qwen3-32b','Qwen3 32B on Groq',131000,0.2900,0.5900),
    ('mdl_qwenplus','Qwen','qwen-plus','Qwen Plus',1000000,0.1150,0.2870),
    ('mdl_qwen35plus','Qwen','qwen3.5-plus','Qwen3.5 Plus',1000000,0.1150,0.6880)
),
inserted as (
  insert into model_catalog(id,provider,model_key,display_name,context_window,capabilities_json,status,created_at)
  select id,provider,model_key,display_name,context_window,'{"chat":true,"stream":true}'::jsonb,'active',now()
  from model_seed
  on conflict do nothing
  returning id
),
updated as (
  update model_catalog m
  set provider=s.provider,
      display_name=s.display_name,
      context_window=s.context_window,
      capabilities_json=m.capabilities_json || '{"chat":true,"stream":true}'::jsonb,
      status='active'
  from model_seed s
  where m.model_key=s.model_key
  returning m.id
)
insert into model_prices(id,model_id,channel_id,input_per_mtok,output_per_mtok,currency,effective_at)
select 'price_' || s.id, m.id, null, s.input_per_mtok, s.output_per_mtok, 'USD', now()
from model_seed s
join model_catalog m on m.model_key=s.model_key
on conflict(id) do update set
  input_per_mtok=excluded.input_per_mtok,
  output_per_mtok=excluded.output_per_mtok,
  currency=excluded.currency,
  effective_at=excluded.effective_at;
