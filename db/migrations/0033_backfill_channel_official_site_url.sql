update channels
set official_site_url = endpoint,
    updated_at = now()
where owner_type = 'platform'
  and deleted_at is null
  and coalesce(official_site_url, '') = ''
  and endpoint ~* '^https?://[^/?#]+/?$'
  and endpoint !~* '^https?://(api|api2|openapi|gateway|proxy|relay|upstream)[.-]';
