package main

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	idb "sehlabs.com/db/internal/db"
)

func speakPlainTextTo(w http.ResponseWriter) {
	w.Header().Add("Content-Type", "text/plain")
}

// func speakJSONTo(w http.ResponseWriter) {
// 	w.Header().Add("Content-Type", "application/json")
// }

func respondWithError(w http.ResponseWriter, err error) {
	statusCode := http.StatusInternalServerError
	if errors.Is(err, idb.ErrTransactionInConflict) {
		statusCode = http.StatusConflict
	}
	speakPlainTextTo(w)
	w.WriteHeader(statusCode)
	fmt.Fprintln(w, err)
}

const pathPrefix = "/record/"

func getTargetKey(w http.ResponseWriter, req *http.Request) (idb.Key, bool) {
	key, ok := strings.CutPrefix(req.URL.Path, pathPrefix)
	if ok && len(key) > 0 {
		return idb.Key(key), true
	}
	speakPlainTextTo(w)
	w.WriteHeader(http.StatusBadRequest)
	fmt.Fprintln(w, "URL path must contain a nonempty key")
	return nil, false
}

func handleGet(ctx context.Context, w http.ResponseWriter, req *http.Request, db database) {
	key, ok := getTargetKey(w, req)
	if !ok {
		return
	}
	var recordExists bool
	var value idb.Value
	if err := db.WithinTransaction(ctx, func(ctx context.Context, tx idb.Transaction) (bool, error) {
		v, err := tx.Get(ctx, key)
		if errors.Is(err, idb.ErrRecordDoesNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		recordExists = true
		v.CopyInto(&value)
		return false, nil
	}); err != nil {
		respondWithError(w, err)
		return
	}
	if !recordExists {
		w.WriteHeader(http.StatusNotFound)
	} else {
		speakPlainTextTo(w)
		if _, err := w.Write(value); err == nil {
			w.Write([]byte{'\n'})
		}
	}
}

func handlePost(ctx context.Context, w http.ResponseWriter, req *http.Request, db database) {
	if err := req.ParseForm(); err != nil {
		speakPlainTextTo(w)
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "Failed to parse HTTP form: %v", err)
		return
	}
	key, ok := getTargetKey(w, req)
	if !ok {
		return
	}
	value := req.FormValue("value")
	var recordExisted bool
	if err := db.WithinTransaction(ctx, func(ctx context.Context, tx idb.Transaction) (bool, error) {
		err := tx.Insert(ctx, key, idb.Value(value))
		if errors.Is(err, idb.ErrRecordExists) {
			recordExisted = true
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return true, nil
	}); err != nil {
		respondWithError(w, err)
	}
	if recordExisted {
		w.WriteHeader(http.StatusConflict)
	} else {
		w.WriteHeader(http.StatusCreated)
	}
}

func handlePut(ctx context.Context, w http.ResponseWriter, req *http.Request, db database) {
	key, ok := getTargetKey(w, req)
	if !ok {
		return
	}
	value := req.FormValue("value")
	type updatePolicy uint
	const (
		abortIfAbsent updatePolicy = iota
		insertIfAbsent
		ignoreIfAbsent
	)
	policy := abortIfAbsent
	{
		const formKey = "if-absent"
		ifAbsent := req.FormValue(formKey)
		switch ifAbsent {
		case "", "abort":
		case "insert":
			policy = insertIfAbsent
		case "ignore":
			policy = ignoreIfAbsent
		default:
			speakPlainTextTo(w)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Unrecognized HTTP form key %q value: %q\n", formKey, ifAbsent)
			return
		}
	}
	if policy == insertIfAbsent {
		// TODO(seh): Implement Upsert.
		w.WriteHeader(http.StatusNotImplemented)
	} else {
		var recordExisted bool
		if err := db.WithinTransaction(ctx, func(ctx context.Context, tx idb.Transaction) (bool, error) {
			err := tx.Update(ctx, key, idb.Value(value))
			if errors.Is(err, idb.ErrRecordDoesNotExist) {
				return false, nil
			}
			if err != nil {
				return false, err
			}
			recordExisted = true
			return true, nil
		}); err != nil {
			respondWithError(w, err)
		}
		if !recordExisted && policy == abortIfAbsent {
			w.WriteHeader(http.StatusNotFound)
		}
	}
}

func handleDelete(ctx context.Context, w http.ResponseWriter, req *http.Request, db database) {
	key, ok := getTargetKey(w, req)
	if !ok {
		return
	}
	var recordExisted bool
	if err := db.WithinTransaction(ctx, func(ctx context.Context, tx idb.Transaction) (bool, error) {
		err, deleted := tx.Delete(ctx, key)
		if err != nil {
			return false, err
		}
		recordExisted = deleted
		return true, nil
	}); err != nil {
		respondWithError(w, err)
		return
	}
	if !recordExisted {
		w.WriteHeader(http.StatusNotFound)
	}
}

func makeHandler(db database) http.Handler {
	var mux http.ServeMux
	{
		mux.Handle(pathPrefix,
			http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				switch req.Method {
				case http.MethodGet:
					handleGet(req.Context(), w, req, db)
				case http.MethodPost:
					handlePost(req.Context(), w, req, db)
				case http.MethodPut:
					handlePut(req.Context(), w, req, db)
				case http.MethodDelete:
					handleDelete(req.Context(), w, req, db)
				default:
					speakPlainTextTo(w)
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(w, "Request uses disallowed HTTP method %q\n", req.Method)
					return
				}
			}))
	}
	return &mux
}
