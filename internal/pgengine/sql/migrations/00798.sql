-- 00798 Add timetable.secret store
--
-- pg_timetable never installs any PostgreSQL extension. This block applies
-- unchanged on a database that has no pgcrypto installed. The extension is
-- looked up at call time inside resolve_secret.
CREATE TABLE timetable.secret (
    client_name  TEXT        NOT NULL,
    secret_name  TEXT        NOT NULL,
    value_enc    BYTEA       NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by   TEXT        NOT NULL DEFAULT session_user,
    PRIMARY KEY (client_name, secret_name)
);

ALTER TABLE timetable.secret ADD CONSTRAINT secret_name_format
    CHECK (secret_name ~ '^[A-Za-z0-9_.-]+$');

COMMENT ON TABLE timetable.secret IS
    'Write-only, named secret values referenced from task parameters and connection strings as ${secret:name}, scoped to exactly one client_name. Modeled on GitHub Actions repository secrets: no plaintext read path, no rotation, no versioning, no cross-client sharing. Requires the pgcrypto extension, which the database administrator installs; pg_timetable itself never does.';
COMMENT ON COLUMN timetable.secret.client_name IS
    'Owning client. Mandatory security boundary: resolvable only by the scheduler process running with this exact client_name (-c/--clientname). Unlike timetable.chain.client_name, NULL/global is not permitted.';
COMMENT ON COLUMN timetable.secret.secret_name IS
    'Reference key used in ${secret:name} syntax, unique within client_name. Case-sensitive, no whitespace (enforced by CHECK secret_name_format).';
COMMENT ON COLUMN timetable.secret.value_enc IS
    'Value encrypted by the operator with pgp_sym_encrypt() from the pgcrypto extension. Decrypted only by timetable.resolve_secret() when supplied the key configured as PGTT_SECRET_KEY, which the database never stores.';
COMMENT ON COLUMN timetable.secret.updated_by IS
    'session_user that last inserted or updated this row, maintained by the secret_touch trigger.';

REVOKE ALL ON timetable.secret FROM PUBLIC;
-- No grants to other roles. Objects are owned by the schema-creating (scheduler)
-- role; a separate administrative role is an operator-managed GRANT.

CREATE OR REPLACE FUNCTION timetable.secret_touch() RETURNS trigger AS
$CODE$
BEGIN
    NEW.updated_at := now();
    NEW.updated_by := session_user;
    RETURN NEW;
END;
$CODE$
LANGUAGE plpgsql;

COMMENT ON FUNCTION timetable.secret_touch() IS
    'Keeps timetable.secret.updated_at/updated_by truthful on UPDATE';

CREATE TRIGGER secret_touch
    BEFORE UPDATE ON timetable.secret
    FOR EACH ROW EXECUTE PROCEDURE timetable.secret_touch();
-- resolve_secret is LANGUAGE plpgsql so the validator does not resolve
-- pgp_sym_decrypt at create time; the extension schema is looked up at call
-- time and interpolated into dynamic SQL so any install layout (public,
-- custom schema, ALTER EXTENSION ... SET SCHEMA) works. A missing extension
-- raises SQLSTATE 0A000 without requiring pgcrypto for create time.
CREATE OR REPLACE FUNCTION timetable.resolve_secret(p_name TEXT, p_client TEXT, p_key TEXT)
RETURNS TEXT
LANGUAGE plpgsql
SECURITY DEFINER
STABLE
STRICT
SET search_path = pg_catalog, timetable
AS $CODE$
DECLARE
    v_enc        BYTEA;
    v_ext_schema TEXT;
    v_plain      TEXT;
BEGIN
    SELECT value_enc INTO v_enc
    FROM timetable.secret
    WHERE client_name = p_client
      AND secret_name = p_name;

    IF NOT FOUND THEN
        RETURN NULL;   -- unknown (client_name, secret_name): no pgcrypto needed
    END IF;

    SELECT n.nspname INTO v_ext_schema
    FROM pg_catalog.pg_extension e
    JOIN pg_catalog.pg_namespace n ON n.oid = e.extnamespace
    WHERE e.extname = 'pgcrypto';

    IF NOT FOUND THEN
        RAISE EXCEPTION 'pgcrypto extension is not installed, cannot decrypt timetable.secret values'
            USING ERRCODE = 'feature_not_supported',
                  HINT = 'Install it (CREATE EXTENSION pgcrypto) or stop using ${secret:...} references';
    END IF;

    EXECUTE format('SELECT %I.pgp_sym_decrypt($1, $2)', v_ext_schema)
       INTO v_plain
      USING v_enc, p_key;

    RETURN v_plain;
END;
$CODE$;

COMMENT ON FUNCTION timetable.resolve_secret(TEXT, TEXT, TEXT) IS
    'Returns the decrypted value of one secret, or NULL when the (client_name, secret_name) pair does not exist. Raises feature_not_supported when pgcrypto is not installed, and Wrong key or corrupt data when the key is wrong.';

REVOKE ALL ON FUNCTION timetable.resolve_secret(TEXT, TEXT, TEXT) FROM PUBLIC;

CREATE OR REPLACE FUNCTION timetable.secret_count()
RETURNS BIGINT
LANGUAGE sql
SECURITY DEFINER
STABLE
SET search_path = pg_catalog, timetable
AS $$
    SELECT count(*) FROM timetable.secret;
$$;

COMMENT ON FUNCTION timetable.secret_count() IS
    'Number of stored secrets, used by the scheduler startup check when no encryption key is configured. Exposes no secret material and does not require pgcrypto.';

REVOKE ALL ON FUNCTION timetable.secret_count() FROM PUBLIC;
