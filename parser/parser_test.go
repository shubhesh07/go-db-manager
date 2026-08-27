package parser

import (
	"reflect"
	"testing"
)

var props = map[string]bool{
	"OrderId": true, "CustomerId": true, "StatusId": true, "Name": true, "Active": true,
	"OrderValue": true, "CreatedOn": true, "CheckIn": true, "VendorCode": true, "Id": true,
}

func resolve(p string) bool { return props[p] }

func TestParse(t *testing.T) {
	cases := []struct {
		name string
		want Tree
	}{
		{"FindByOrderId", Tree{Verb: Find, Or: [][]Part{{{"OrderId", "Equals", 1, false}}}, NumArgs: 1}},
		{"FindByVendorCodeAndOrderId", Tree{Verb: Find, Or: [][]Part{{{"VendorCode", "Equals", 1, false}, {"OrderId", "Equals", 1, false}}}, NumArgs: 2}},
		{"FindByCustomerIdOrStatusId", Tree{Verb: Find, Or: [][]Part{{{"CustomerId", "Equals", 1, false}}, {{"StatusId", "Equals", 1, false}}}, NumArgs: 2}},
		{"FindByOrderIdIn", Tree{Verb: Find, Or: [][]Part{{{"OrderId", "In", 1, false}}}, NumArgs: 1}},
		{"FindByStatusIdNotIn", Tree{Verb: Find, Or: [][]Part{{{"StatusId", "NotIn", 1, false}}}, NumArgs: 1}},
		{"FindByCheckIn", Tree{Verb: Find, Or: [][]Part{{{"CheckIn", "Equals", 1, false}}}, NumArgs: 1}},
		{"FindByNameContaining", Tree{Verb: Find, Or: [][]Part{{{"Name", "Containing", 1, false}}}, NumArgs: 1}},
		{"FindByNameStartingWithIgnoreCase", Tree{Verb: Find, Or: [][]Part{{{"Name", "StartingWith", 1, true}}}, NumArgs: 1}},
		{"FindByOrderValueGreaterThanEqual", Tree{Verb: Find, Or: [][]Part{{{"OrderValue", "GreaterThanEqual", 1, false}}}, NumArgs: 1}},
		{"FindByStatusIdNot", Tree{Verb: Find, Or: [][]Part{{{"StatusId", "Not", 1, false}}}, NumArgs: 1}},
		{"FindByActiveTrue", Tree{Verb: Find, Or: [][]Part{{{"Active", "True", 0, false}}}}},
		{"FindByNameIsNull", Tree{Verb: Find, Or: [][]Part{{{"Name", "IsNull", 0, false}}}}},
		{"FindByNameNotNull", Tree{Verb: Find, Or: [][]Part{{{"Name", "IsNotNull", 0, false}}}}},
		{"FindByOrderValueBetween", Tree{Verb: Find, Or: [][]Part{{{"OrderValue", "Between", 2, false}}}, NumArgs: 2}},
		{"FindByCreatedOnAfter", Tree{Verb: Find, Or: [][]Part{{{"CreatedOn", "GreaterThan", 1, false}}}, NumArgs: 1}},
		{"FindTop5ByCustomerIdOrderByCreatedOnDesc", Tree{Verb: Find, Limit: 5, Or: [][]Part{{{"CustomerId", "Equals", 1, false}}}, OrderBy: []Order{{"CreatedOn", true}}, NumArgs: 1}},
		{"FindFirstByOrderByOrderIdDesc", Tree{Verb: Find, Limit: 1, OrderBy: []Order{{"OrderId", true}}}},
		{"FindDistinctByCustomerIdOrderByOrderIdDescCreatedOnAsc", Tree{Verb: Find, Distinct: true, Or: [][]Part{{{"CustomerId", "Equals", 1, false}}}, OrderBy: []Order{{"OrderId", true}, {"CreatedOn", false}}, NumArgs: 1}},
		{"FindByCustomerIdOrderByCreatedOn", Tree{Verb: Find, Or: [][]Part{{{"CustomerId", "Equals", 1, false}}}, OrderBy: []Order{{"CreatedOn", false}}, NumArgs: 1}},
		{"CountByStatusId", Tree{Verb: Count, Or: [][]Part{{{"StatusId", "Equals", 1, false}}}, NumArgs: 1}},
		{"ExistsByOrderId", Tree{Verb: Exists, Or: [][]Part{{{"OrderId", "Equals", 1, false}}}, NumArgs: 1}},
		{"DeleteByOrderId", Tree{Verb: Delete, Or: [][]Part{{{"OrderId", "Equals", 1, false}}}, NumArgs: 1}},
		{"RemoveByOrderId", Tree{Verb: Delete, Or: [][]Part{{{"OrderId", "Equals", 1, false}}}, NumArgs: 1}},
		{"FindAll", Tree{Verb: Find}},
		{"GetActiveOrdersByCustomerId", Tree{Verb: Find, Or: [][]Part{{{"CustomerId", "Equals", 1, false}}}, NumArgs: 1}},
		{"FindByCustomerIdAndNameAllIgnoreCase", Tree{Verb: Find, Or: [][]Part{{{"CustomerId", "Equals", 1, true}, {"Name", "Equals", 1, true}}}, NumArgs: 2}},
	}
	for _, c := range cases {
		got, err := Parse(c.name, resolve)
		if err != nil {
			t.Errorf("%s: %v", c.name, err)
			continue
		}
		if !reflect.DeepEqual(*got, c.want) {
			t.Errorf("%s:\n got %+v\nwant %+v", c.name, *got, c.want)
		}
	}
}

func TestParseErrors(t *testing.T) {
	for _, name := range []string{"FindByNope", "SaveByOrderId", "FindByOrderIdOrderByNope", "FindByAnd"} {
		if _, err := Parse(name, resolve); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}
