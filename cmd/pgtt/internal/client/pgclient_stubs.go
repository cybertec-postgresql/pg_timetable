package client

// compile-time assertion that PgClient satisfies Client.
var _ Client = (*PgClient)(nil)
