with model_seed(id, provider, model_key, display_name, context_window, input_per_mtok, output_per_mtok) as (
  values
    ('mdl_gpt55','OpenAI','gpt-5.5','GPT-5.5',1050000,5.0000,30.0000)
),
upserted as (
  insert into model_catalog(id,provider,model_key,display_name,context_window,capabilities_json,status,created_at)
  select id,provider,model_key,display_name,context_window,'{"chat":true,"stream":true,"vision":true}'::jsonb,'active',now()
  from model_seed
  on conflict(model_key) do update set
    provider=excluded.provider,
    display_name=excluded.display_name,
    context_window=excluded.context_window,
    capabilities_json=model_catalog.capabilities_json || excluded.capabilities_json,
    status='active'
  returning id, model_key
)
insert into model_prices(id,model_id,channel_id,input_per_mtok,output_per_mtok,currency,effective_at)
select 'price_' || s.id, u.id, null, s.input_per_mtok, s.output_per_mtok, 'USD', now()
from model_seed s
join upserted u on u.model_key=s.model_key
on conflict(id) do update set
  input_per_mtok=excluded.input_per_mtok,
  output_per_mtok=excluded.output_per_mtok,
  currency=excluded.currency,
  effective_at=excluded.effective_at;
