CREATE OR REPLACE FUNCTION cfa_parse_rfc3339_utc_timestamp(value TEXT)
RETURNS TIMESTAMPTZ
LANGUAGE plpgsql
IMMUTABLE
AS $$
DECLARE
  normalized_value TEXT;
  parsed TIMESTAMPTZ;
BEGIN
  normalized_value := BTRIM(value);
  IF normalized_value IS NULL OR normalized_value = '' THEN
    RETURN NULL;
  END IF;
  IF normalized_value !~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})$' THEN
    RETURN NULL;
  END IF;

  BEGIN
    parsed := REPLACE(normalized_value, 'Z', '+00:00')::TIMESTAMPTZ;
  EXCEPTION WHEN OTHERS THEN
    RETURN NULL;
  END;

  RETURN parsed;
END;
$$;
