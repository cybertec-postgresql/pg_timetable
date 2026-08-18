package pgengine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/cybertec-postgresql/pg_timetable/internal/log"
	"github.com/jackc/pgx/v5/pgconn"
)

// secretRefPattern matches ${secret:name}; the character class mirrors the
// secret_name_format CHECK constraint (REQ-021, REQ-025).
var secretRefPattern = regexp.MustCompile(`\$\{secret:([A-Za-z0-9_.-]+)\}`)

const secretRefSubstring = "${secret:"

// resolveSecretSQL calls timetable.resolve_secret with the fixed client scope.
// The context is wrapped with log.WithoutQueryArgs so the encryption key never
// reaches the pgx tracer (REQ-018, REQ-030).
const resolveSecretSQL = `SELECT timetable.resolve_secret($1, $2, $3)`

// resolveRefs is the shared engine used by both ResolveSecretsJSON and
// ResolveSecretsConnString (REQ-018, REQ-022, REQ-024, REQ-025, REQ-040).
//
// If s does not contain the literal substring `${secret:`, it is returned
// byte-identical with no parsing, no regexp evaluation, and no database call
// (REQ-026, CON-002). Otherwise, each match is resolved exactly once against
// timetable.resolve_secret using pge.ClientName as the fixed scope, and quote
// is applied to the resolved value before substitution. Resolved values are
// never re-scanned (REQ-024).
func (pge *PgEngine) resolveRefs(
	ctx context.Context, s string,
	quote func(value string, m []int, in string) string,
) (string, []string, error) {
	if !strings.Contains(s, secretRefSubstring) {
		return s, nil, nil
	}
	// Pre-flight: if any reference is present and the key is empty, fail fast
	// with a descriptive error (REQ-041 class 2).
	if pge.SecretEncryptionKey == "" {
		return "", uniqueRefNames(s), fmt.Errorf(
			"secret references found (%s) but SecretEncryptionKey is not configured; set PGTT_SECRET_KEY/--secret-key",
			strings.Join(uniqueRefNames(s), ", "))
	}
	markedCtx := log.WithoutQueryArgs(ctx)
	var (
		out   strings.Builder
		names []string
		last  int
	)
	for _, m := range secretRefPattern.FindAllStringSubmatchIndex(s, -1) {
		name := s[m[2]:m[3]]
		var plaintext *string
		err := pge.ConfigDb.QueryRow(markedCtx, resolveSecretSQL, name, pge.ClientName, pge.SecretEncryptionKey).Scan(&plaintext)
		if err != nil {
			// REQ-041 class 3: pgp_sym_decrypt raises "Wrong key or corrupt data".
			// Surface it wrapped with the secret name; never as not-found.
			if isWrongKey(err) {
				return "", append(names, name), fmt.Errorf(
					`secret %q: wrong key or corrupt data`, name)
			}
			// REQ-041 class 4: pgcrypto is absent. resolve_secret raises
			// feature_not_supported (SQLSTATE 0A000) per REQ-008. Wrap with
			// the secret name and a statement that installing pgcrypto is the
			// database administrator's responsibility. This is a per-task
			// error only — never a startup failure (REQ-054).
			if isMissingPgcrypto(err) {
				return "", append(names, name), fmt.Errorf(
					`secret %q: pgcrypto extension is not installed; `+
						`installing it is the database administrator's responsibility`,
					name)
			}
			return "", append(names, name), fmt.Errorf(
				"secret %q: %w", name, err)
		}
		if plaintext == nil {
			// Missing secret (one row containing NULL). Indistinguishable
			// across client scopes (REQ-041 class 1, REQ-044).
			return "", append(names, name), fmt.Errorf(
				`secret %q not found for client %q`, name, pge.ClientName)
		}
		out.WriteString(s[last:m[0]])
		out.WriteString(quote(*plaintext, m, s))
		names = append(names, name)
		last = m[1]
	}
	out.WriteString(s[last:])
	return out.String(), uniqueRefNames(strings.Join(names, ",")), nil
}

// isWrongKey reports whether the error originates from pgp_sym_decrypt's
// "Wrong key or corrupt data" failure (REQ-041 class 3).
func isWrongKey(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Wrong key or corrupt data") ||
		strings.Contains(msg, "wrong key or corrupt data")
}

// isMissingPgcrypto reports whether the error is the SQLSTATE 0A000
// (feature_not_supported) raised by timetable.resolve_secret when the
// pgcrypto extension is not installed (REQ-041 class 4, REQ-008).
func isMissingPgcrypto(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "0A000"
	}
	return false
}

func uniqueRefNames(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	seen := make(map[string]struct{}, len(parts))
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

// ResolveSecretsJSON resolves ${secret:name} references inside the string
// leaves of a jsonb-encoded parameter value and returns the re-encoded JSON
// (REQ-027, REQ-029, AC-008). Resolved values are never re-scanned (REQ-024).
//
// If s does not contain the literal substring "${secret:" it is returned
// byte-identical with no parsing, no database call (REQ-026, CON-002).
func (pge *PgEngine) ResolveSecretsJSON(ctx context.Context, s string) (resolved string, names []string, err error) {
	if !strings.Contains(s, secretRefSubstring) {
		return s, nil, nil
	}
	if pge.SecretEncryptionKey == "" {
		return "", nil, fmt.Errorf(
			"secret references found but SecretEncryptionKey is not configured; set PGTT_SECRET_KEY/--secret-key")
	}
	var doc any
	if derr := json.Unmarshal([]byte(s), &doc); derr != nil {
		return "", nil, fmt.Errorf("resolve json: %w", derr)
	}
	var (
		allNames []string
		walkErr  error
	)
	walkJSON(doc, func(v *string) {
		if v == nil {
			return
		}
		// resolveRefs short-circuits on no substring; safe to call per leaf.
		sub, names, rerr := pge.resolveRefs(ctx, *v, quoteJSONLeaf)
		if rerr != nil {
			walkErr = errors.Join(walkErr, rerr)
			return
		}
		*v = sub
		allNames = append(allNames, names...)
	})
	if walkErr != nil {
		return "", uniqueRefNames(strings.Join(allNames, ",")), walkErr
	}
	out, merr := json.Marshal(doc)
	if merr != nil {
		return "", uniqueRefNames(strings.Join(allNames, ",")), fmt.Errorf("encode resolved json: %w", merr)
	}
	return string(out), uniqueRefNames(strings.Join(allNames, ",")), nil
}

// quoteJSONLeaf is the no-op quote function used by ResolveSecretsJSON: the
// jsonb substitution happens inside the JSON walker, so the value is inserted
// verbatim. The secret's escaping is performed by json.Marshal.
func quoteJSONLeaf(value string, _ []int, _ string) string {
	return value
}

// walkJSON invokes fn on every JSON string leaf reachable from v. v is
// mutated in place.
func walkJSON(v any, fn func(*string)) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			if s, ok := child.(string); ok {
				if strings.Contains(s, secretRefSubstring) {
					p := s
					fn(&p)
					x[k] = p
				}
			} else {
				walkJSON(child, fn)
			}
		}
	case []any:
		for i, child := range x {
			if s, ok := child.(string); ok {
				if strings.Contains(s, secretRefSubstring) {
					p := s
					fn(&p)
					x[i] = p
				}
			} else {
				walkJSON(child, fn)
			}
		}
	}
}

// ResolveSecretsConnString resolves ${secret:name} references inside a libpq
// conninfo string, applying conninfo quoting to each resolved value per
// REQ-028. Same short-circuit contract as ResolveSecretsJSON (REQ-026).
func (pge *PgEngine) ResolveSecretsConnString(ctx context.Context, s string) (resolved string, names []string, err error) {
	return pge.resolveRefs(ctx, s, func(value string, m []int, in string) string {
		return quoteConnInfoValue(value, in, m)
	})
}

// quoteConnInfoValue applies libpq conninfo quoting to a resolved secret
// value. If the reference in the template is already delimited by single
// quotes (e.g. `password='${secret:pw}'`), the wrapping is omitted and only
// `\` and `'` are escaped, so the existing delimiters are not doubled
// (REQ-028). An empty value is emitted as `''` because a bare `password=`
// followed by whitespace would swallow the next token (REQ-028).
func quoteConnInfoValue(value string, in string, m []int) string {
	if value == "" {
		return "''"
	}
	needsQuoting := strings.ContainsAny(value, " \t\n'\\")
	if !needsQuoting {
		return value
	}
	if isAlreadySingleQuoted(in, m) {
		return escapeConnInfo(value)
	}
	return "'" + escapeConnInfo(value) + "'"
}

func escapeConnInfo(v string) string {
	r := strings.NewReplacer(`\`, `\\`, `'`, `\'`)
	return r.Replace(v)
}

// isAlreadySingleQuoted returns true when the bytes immediately surrounding
// the reference form a `password='…'` style template.
func isAlreadySingleQuoted(in string, m []int) bool {
	if m[0] == 0 || m[1] >= len(in) {
		return false
	}
	return in[m[0]-1] == '\'' && in[m[1]] == '\''
}

// CheckSecretConfig logs an error when timetable.secret contains rows but no
// encryption key is configured (REQ-013, REQ-019, REQ-020, CON-002). It
// performs no query when SecretEncryptionKey is non-empty. A failure of the
// check itself is logged, never fatal (REQ-020).
func (pge *PgEngine) CheckSecretConfig(ctx context.Context) error {
	if pge.SecretEncryptionKey != "" {
		return nil
	}
	markedCtx := log.WithoutQueryArgs(ctx)
	var count int64
	if err := pge.ConfigDb.QueryRow(markedCtx, `SELECT timetable.secret_count()`).Scan(&count); err != nil {
		pge.l.WithError(err).Error("Cannot check timetable.secret configuration")
		return nil
	}
	if count > 0 {
		pge.l.WithField("secret_count", count).Error(
			"timetable.secret contains rows but SecretEncryptionKey is not configured; set PGTT_SECRET_KEY/--secret-key")
	}
	return nil
}
