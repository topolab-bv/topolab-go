package topolab

import "context"

// SQLOptions configure a SQL query. The zero value leaves the row cap to the
// server.
type SQLOptions struct {
	MaxRows int // 1–10000; 0 omits the field and uses the server default
}

// sqlRequest is the POST /v1/sql/query payload.
type sqlRequest struct {
	SQL     string `json:"sql"`
	MaxRows int    `json:"maxRows,omitempty"`
}

// SQL runs one read-only SQL query across the datasets the organization
// licences. It is client-level rather than per-dataset because a query may join
// several of them.
//
// The query must be a single read-only SELECT (or WITH): multiple statements,
// DDL and DML are rejected before execution, and every relation the planner
// resolves must be a dataset the organization holds an active licence for.
//
// Requires the sql-access entitlement, which is part of the Enterprise plan and
// is not sold separately; without it the call fails with ErrAddonRequired. A
// query that outruns the server statement timeout fails with ErrQueryTimeout.
func (c *Client) SQL(ctx context.Context, query string, opts *SQLOptions) (*SQLResult, error) {
	req := sqlRequest{SQL: query}
	if opts != nil && opts.MaxRows > 0 {
		req.MaxRows = opts.MaxRows
	}
	var res SQLResult
	if err := c.t.postJSON(ctx, "/v1/sql/query", req, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
