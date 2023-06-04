package db

import (
	"bytes"
	"context"
	"errors"
	"testing"
)

func confirmRecordIsAbsent(ctx context.Context, t *testing.T, store *ShardedStore, key Key) {
	t.Helper()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (bool, error) {
		v, err := tx.Get(ctx, key)
		if !errors.Is(err, ErrRecordDoesNotExist) {
			t.Error(err)
		}
		if want, got := []byte{}, v; !bytes.Equal(want, got) {
			t.Errorf("record value: want %q, got %q", want, got)
		}
		// Don't bother trying to commit anything.
		return false, nil
	}); err != nil {
		t.Error(err)
	}
}

func confirmRecordIsPresentIn(ctx context.Context, t *testing.T, tx Transaction, key Key, value Value) {
	t.Helper()
	v, err := tx.Get(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	if want, got := value, v; !bytes.Equal(want, got) {
		t.Errorf("record value: want %q, got %q", want, got)
	}
}

func confirmRecordIsPresent(ctx context.Context, t *testing.T, store *ShardedStore, key Key, value Value) {
	t.Helper()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (bool, error) {
		confirmRecordIsPresentIn(ctx, t, tx, key, value)
		// Don't bother trying to commit anything.
		return false, nil
	}); err != nil {
		t.Error(err)
	}
}

func TestGetAbsentRecord(t *testing.T) {
	store, err := MakeShardedStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (bool, error) {
		key := Key("k1")
		v, err := tx.Get(ctx, key)
		if !errors.Is(err, ErrRecordDoesNotExist) {
			t.Error(err)
		}
		if want, got := 0, len(v); want != got {
			t.Errorf("value length: want %d, got %d", want, got)
		}
		return false, nil
	}); err != nil {
		t.Error(err)
	}
}

func TestInsertGetCommitGet(t *testing.T) {
	store, err := MakeShardedStore()
	if err != nil {
		t.Fatal(err)
	}
	key := Key("k1")
	value := Value("v1")
	ctx := context.Background()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (bool, error) {
		if err := tx.Insert(ctx, key, value); err != nil {
			t.Fatal(err)
		}
		confirmRecordIsPresentIn(ctx, t, tx, key, value)
		return true, nil
	}); err != nil {
		t.Error(err)
	}
	// Now confirm that the changes were committed, visible to subsequent transactions.
	confirmRecordIsPresent(ctx, t, store, key, value)
}

func TestInsertGetAbortGet(t *testing.T) {
	store, err := MakeShardedStore()
	if err != nil {
		t.Fatal(err)
	}
	key := Key("k1")
	ctx := context.Background()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (bool, error) {
		value := Value("v1")
		if err := tx.Insert(ctx, key, value); err != nil {
			t.Fatal(err)
		}
		confirmRecordIsPresentIn(ctx, t, tx, key, value)
		return false, nil
	}); err != nil {
		t.Error(err)
	}
	// Now confirm that the changes were not committed, and are not visible to subsequent transactions.
	confirmRecordIsAbsent(ctx, t, store, key)
}

func TestInsertInsertCommitGet(t *testing.T) {
	store, err := MakeShardedStore()
	if err != nil {
		t.Fatal(err)
	}
	key := Key("k1")
	value := Value("v1")
	ctx := context.Background()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (commit bool, err error) {
		if err := tx.Insert(ctx, key, value); err != nil {
			t.Fatal(err)
		}
		// A second attempt to insert the same record in the same transaction should fail, because
		// we can see the pending record as existing.
		if err := tx.Insert(ctx, key, value); !errors.Is(err, ErrRecordExists) {
			t.Error(err)
		}
		return true, nil
	}); err != nil {
		t.Error(err)
	}
	// Now confirm that the changes were committed, visible to subsequent transactions.
	confirmRecordIsPresent(ctx, t, store, key, value)
}

func TestInsertDeleteInsertGetAbortGet(t *testing.T) {
	store, err := MakeShardedStore()
	if err != nil {
		t.Fatal(err)
	}
	key := Key("k1")
	ctx := context.Background()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (commit bool, err error) {
		value := Value("v1")
		if err := tx.Insert(ctx, key, value); err != nil {
			t.Fatal(err)
		}
		err, deleted := tx.Delete(ctx, key)
		if err != nil {
			t.Fatal(err)
		}
		if !deleted {
			t.Error("record deleted: want true, got false")
		}
		if err := tx.Insert(ctx, key, value); err != nil {
			t.Fatal(err)
		}
		return false, nil
	}); err != nil {
		t.Error(err)
	}
	// Now confirm that the changes were not committed, and are not visible to subsequent transactions.
	confirmRecordIsAbsent(ctx, t, store, key)
}

func TestUpdate(t *testing.T) {
	store, err := MakeShardedStore()
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (commit bool, err error) {
		key := Key("k1")
		if _, err := tx.Get(ctx, key); !errors.Is(err, ErrRecordDoesNotExist) {
			t.Fatal(err)
		}
		// Since the record does not exist, we should not be allowed to update it.
		if err := tx.Update(ctx, key, Value("v1")); !errors.Is(err, ErrRecordDoesNotExist) {
			t.Fatal(err)
		}
		return false, nil
	}); err != nil {
		t.Error(err)
	}
}

func TestInsertUpdateCommitGet(t *testing.T) {
	store, err := MakeShardedStore()
	if err != nil {
		t.Fatal(err)
	}
	key := Key("k1")
	subsequentValue := Value("v2")
	ctx := context.Background()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (commit bool, err error) {
		initialValue := Key("v1")
		if err := tx.Insert(ctx, key, Value(initialValue)); err != nil {
			t.Fatal(err)
		}
		err = tx.Update(ctx, key, subsequentValue)
		if err != nil {
			t.Fatal(err)
		}
		return true, nil
	}); err != nil {
		t.Error(err)
	}
	// Now confirm that the changes were committed, visible to subsequent transactions.
	confirmRecordIsPresent(ctx, t, store, key, subsequentValue)
}

func TestInsertUpdateGetUpdateGetAbortGet(t *testing.T) {
	store, err := MakeShardedStore()
	if err != nil {
		t.Fatal(err)
	}
	key := Key("k1")
	ctx := context.Background()
	if err := store.WithinTransaction(ctx, func(ctx context.Context, tx Transaction) (commit bool, err error) {
		initialValue := Value("v1")
		if err := tx.Insert(ctx, key, initialValue); err != nil {
			t.Fatal(err)
		}
		secondValue := Value("v2")
		err = tx.Update(ctx, key, secondValue)
		if err != nil {
			t.Fatal(err)
		}
		confirmRecordIsPresentIn(ctx, t, tx, key, secondValue)
		thirdValue := Value("v3")
		err = tx.Update(ctx, key, thirdValue)
		if err != nil {
			t.Fatal(err)
		}
		confirmRecordIsPresentIn(ctx, t, tx, key, thirdValue)
		return false, nil
	}); err != nil {
		t.Error(err)
	}
	// Now confirm that the changes were not committed, and are not visible to subsequent transactions.
	confirmRecordIsAbsent(ctx, t, store, key)
}
