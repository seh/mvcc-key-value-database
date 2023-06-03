package main

import (
	"fmt"
	"net/http"
	"strings"
)

func speakPlainTextTo(w http.ResponseWriter) {
	w.Header().Add("Content-Type", "text/plain")
}

// func speakJSONTo(w http.ResponseWriter) {
// 	w.Header().Add("Content-Type", "application/json")
// }

func makeHandler(db database) http.Handler {
	var mux http.ServeMux
	{
		const pathPrefix = "/record/"
		mux.Handle(pathPrefix,
			http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				getTargetKey := func() (string, bool) {
					key, ok := strings.CutPrefix(req.URL.Path, pathPrefix)
					if ok && len(key) > 0 {
						return key, true
					}
					w.WriteHeader(http.StatusBadRequest)
					// TODO(seh): Write this as JSON.
					speakPlainTextTo(w)
					fmt.Fprintln(w, "URL path must contain a nonempty key")
					return "", false
				}
				switch req.Method {
				case http.MethodPost:
					if err := req.ParseForm(); err != nil {
						w.WriteHeader(http.StatusBadRequest)
						// TODO(seh): Write this as JSON.
						speakPlainTextTo(w)
						fmt.Fprintf(w, "Failed to parse HTTP form: %v", err)
						return
					}
					key, ok := getTargetKey()
					if !ok {
						return
					}
					value := req.FormValue("value")
					// TODO(seh): Implement Insert.
					// TODO(seh): Return status code 201 if we inserted a record.
					fmt.Fprintf(w, "Key: %q -> Value: %q\n", key, value)
					return
				case http.MethodPut:
					key, ok := getTargetKey()
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
							w.WriteHeader(http.StatusBadRequest)
							// TODO(seh): Write this as JSON.
							speakPlainTextTo(w)
							fmt.Fprintf(w, "Unrecognized HTTP form key %q value: %q\n", formKey, ifAbsent)
							return
						}
					}
					fmt.Fprintf(w, "Key: %q -> Value: %q\n", key, value)
					if policy == insertIfAbsent {
						// TODO(seh): Implement Upsert.
					} else {
						// TODO(seh): Implement Update, taking the policy into account when handling
						// the db.ErrRecordDoesNotExist error.
					}
				case http.MethodDelete:
					/*key*/ _, ok := getTargetKey()
					if !ok {
						return
					}
					// TODO(seh): Implement Delete.
				}
			}))
	}
	return &mux
}
