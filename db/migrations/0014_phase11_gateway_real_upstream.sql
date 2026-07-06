alter table gateway_request_events add column if not exists usage_estimated boolean not null default false;

create index if not exists idx_gateway_events_usage_estimated on gateway_request_events(usage_estimated, created_at desc);
