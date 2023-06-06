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

const pathPrefixSingleRecord = "/record/"

func getTargetKey(w http.ResponseWriter, req *http.Request) (idb.Key, bool) {
	key, ok := strings.CutPrefix(req.URL.Path, pathPrefixSingleRecord)
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
		fmt.Fprintf(w, "Failed to parse HTTP form: %v\n", err)
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
		if err := db.WithinTransaction(ctx, func(ctx context.Context, tx idb.Transaction) (bool, error) {
			err := tx.Upsert(ctx, key, idb.Value(value))
			return err != nil, err
		}); err != nil {
			respondWithError(w, err)
		}
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
	type deletePolicy uint
	const (
		abortIfAbsent deletePolicy = iota
		ignoreIfAbsent
	)
	policy := abortIfAbsent
	{
		const formKey = "if-absent"
		ifAbsent := req.FormValue(formKey)
		switch ifAbsent {
		case "", "abort":
		case "ignore":
			// Treat these requests idempotently, with the intention of merely ensuring that the
			// record with the given key does not exist, whether or not this request made it so.
			policy = ignoreIfAbsent
		default:
			speakPlainTextTo(w)
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprintf(w, "Unrecognized HTTP form key %q value: %q\n", formKey, ifAbsent)
			return
		}
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
	if !recordExisted && policy == abortIfAbsent {
		w.WriteHeader(http.StatusNotFound)
	}
}

func makeHandler(db database) http.Handler {
	var mux http.ServeMux
	{
		mux.Handle(pathPrefixSingleRecord,
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
		mux.Handle("/records/batch",
			http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				if req.Method != http.MethodPost {
					speakPlainTextTo(w)
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(w, "Request uses disallowed HTTP method %q\n", req.Method)
					return
				}
				if err := req.ParseForm(); err != nil {
					speakPlainTextTo(w)
					w.WriteHeader(http.StatusBadRequest)
					fmt.Fprintf(w, "Failed to parse HTTP form: %v\n", err)
					return
				}
				absentFormEntries := req.Form["absent"]
				boundFormEntries := req.Form["bound"]
				bindings := make(map[string]*idb.Value, len(absentFormEntries)+len(boundFormEntries))
				for _, k := range absentFormEntries {
					if len(k) == 0 {
						continue
					}
					bindings[k] = nil
				}
				for _, v := range boundFormEntries {
					if len(v) < 3 {
						continue
					}
					delim := v[:1]
					if before, after, ok := strings.Cut(v[1:], delim); ok && len(before) > 0 {
						if _, ok := bindings[before]; ok {
							speakPlainTextTo(w)
							w.WriteHeader(http.StatusBadRequest)
							fmt.Fprintf(w, "HTTP form requests ensuring key %q is both bound and absent\n", before)
							return
						}
						value := idb.Value(after)
						bindings[before] = &value
					}
				}
				if len(bindings) == 0 {
					return
				}
				if err := db.WithinTransaction(req.Context(), func(ctx context.Context, tx idb.Transaction) (bool, error) {
					for key, value := range bindings {
						var err error
						if value == nil {
							err, _ = tx.Delete(ctx, idb.Key(key))
						} else {
							err = tx.Upsert(ctx, idb.Key(key), *value)
						}
						if err != nil {
							return false, err
						}
					}
					return true, nil
				}); err != nil {
					respondWithError(w, err)
				}
			}))
	}
	return &mux
}
