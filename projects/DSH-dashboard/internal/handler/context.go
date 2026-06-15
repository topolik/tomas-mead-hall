package handler

import (
	"context"
	"net/http"
)

func withValue(ctx context.Context, key, val any) context.Context {
	return context.WithValue(ctx, key, val)
}

func userIDFromRequest(r *http.Request) (int64, bool) {
	v := r.Context().Value(ctxUserID)
	if v == nil {
		return 0, false
	}
	id, ok := v.(int64)
	return id, ok
}
