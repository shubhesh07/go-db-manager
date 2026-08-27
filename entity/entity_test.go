package entity

import (
	"reflect"
	"testing"
)

type base struct {
	ID        int64 `db:"id" orm:"pk,auto"`
	UpdatedBy int64 `db:"updated_by" orm:"updated_by"`
}

type thing struct {
	base
	ProductCode string `db:"product_code"`
	KeepOrig    bool   `db:"keep_orginal"` // misspelled production column
	Ignored     string `db:"-"`
	Rel         *thing // relationship, not a column
	HTTPCode    int
}

func TestMeta(t *testing.T) {
	m, err := Of[thing]()
	if err != nil {
		t.Fatal(err)
	}
	if m.Table != "thing" || m.PK == nil || m.PK.Name != "id" || !m.PK.Auto {
		t.Errorf("%+v", m)
	}
	want := []string{"id", "updated_by", "product_code", "keep_orginal", "http_code"}
	got := m.ColumnNames()
	if len(got) != len(want) {
		t.Fatalf("columns %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("col %d = %s want %s", i, got[i], want[i])
		}
	}
	for _, p := range []string{"Id", "ID", "ProductCode", "KeepOrig", "KeepOrginal", "HttpCode", "UpdatedBy"} {
		if _, ok := m.ResolveProperty(p); !ok {
			t.Errorf("%s should resolve", p)
		}
	}
	if _, ok := m.ResolveProperty("Rel"); ok {
		t.Error("Rel must not resolve")
	}
}

func TestSnakeCase(t *testing.T) {
	for in, want := range map[string]string{"ProductCode": "product_code", "WarehouseID": "warehouse_id", "HTTPCode": "http_code", "ID": "id", "A": "a", "mrp": "mrp"} {
		if got := SnakeCase(in); got != want {
			t.Errorf("%s -> %s want %s", in, got, want)
		}
	}
}

func TestCoerceInt(t *testing.T) {
	var v struct {
		A int64
		B uint8
		C *int
	}
	f := reflect.ValueOf(&v).Elem()
	for i, src := range []any{[]byte{1}, []byte("7"), int64(3)} {
		if err := (&coerce{f.Field(i)}).Scan(src); err != nil {
			t.Fatal(err)
		}
	}
	if v.A != 1 || v.B != 7 || v.C == nil || *v.C != 3 {
		t.Errorf("%+v", v)
	}
	if err := (&coerce{f.Field(0)}).Scan([]byte{0, 0, 0, 0, 0, 0, 0, 0, 1}); err == nil {
		t.Error("9-byte BIT must fail")
	}
}
