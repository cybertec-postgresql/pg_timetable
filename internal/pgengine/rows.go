package pgengine

import (
	"fmt"
	"reflect"
	"strings"

	pgx "github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// RowToStructByName returns a T scanned from row. T must be a struct. T must have the same number a named public fields as row
// has fields. The row and T fields will by matched by name.
func RowToStructByName[T any](row pgx.CollectableRow) (T, error) {
	var value T
	err := row.Scan(&namedStructRowScanner{ptrToStruct: &value})
	return value, err
}

// RowToAddrOfStructByPos returns the address of a T scanned from row. T must be a struct. T must have the same number a
// named public fields as row has fields. The row and T fields will by matched by name.
func RowToAddrOfStructByName[T any](row pgx.CollectableRow) (*T, error) {
	var value T
	err := row.Scan(&namedStructRowScanner{ptrToStruct: &value})
	return &value, err
}

type namedStructRowScanner struct {
	ptrToStruct any
}

func (rs *namedStructRowScanner) ScanRow(rows pgx.Rows) error {
	dst := rs.ptrToStruct
	dstValue := reflect.ValueOf(dst)
	if dstValue.Kind() != reflect.Ptr {
		return fmt.Errorf("dst not a pointer")
	}

	dstElemValue := dstValue.Elem()
	scanTargets, err := rs.appendScanTargets(dstElemValue, nil, rows.FieldDescriptions())

	if err != nil {
		return err
	}

	for i, t := range scanTargets {
		if t == nil {
			return fmt.Errorf("struct doesn't have corresponding row field %s", rows.FieldDescriptions()[i].Name)
		}
	}

	return rows.Scan(scanTargets...)
}

const structTagKey = "db"

func fieldPosByName(fldDescs []pgconn.FieldDescription, field string) (i int) {
	i = -1
	for i, desc := range fldDescs {
		if strings.EqualFold(desc.Name, field) {
			return i
		}
	}
	return
}

func (rs *namedStructRowScanner) appendScanTargets(dstElemValue reflect.Value, scanTargets []any, fldDescs []pgconn.FieldDescription) ([]any, error) {
	var err error
	dstElemType := dstElemValue.Type()

	if scanTargets == nil {
		scanTargets = make([]any, len(fldDescs))
	}

	for i := 0; i < dstElemType.NumField(); i++ {
		sf := dstElemType.Field(i)
		if sf.PkgPath != "" && !sf.Anonymous {
			// Field is unexported, skip it.
			continue
		}
		// Handle anoymous struct embedding, but do not try to handle embedded pointers.
		if sf.Anonymous && sf.Type.Kind() == reflect.Struct {
			scanTargets, err = rs.appendScanTargets(dstElemValue.Field(i), scanTargets, fldDescs)
			if err != nil {
				return nil, err
			}
		} else {
			dbTag, dbTagPresent := sf.Tag.Lookup(structTagKey)
			if dbTagPresent {
				dbTag = strings.Split(dbTag, ",")[0]
			}
			if dbTag == "-" {
				// Field is ignored, skip it.
				continue
			}
			colName := dbTag
			if !dbTagPresent {
				colName = sf.Name
			}
			fpos := fieldPosByName(fldDescs, colName)
			if fpos == -1 || fpos >= len(scanTargets) {
				return nil, fmt.Errorf("cannot find field %s in returned row", colName)
			}
			scanTargets[fpos] = dstElemValue.Field(i).Addr().Interface()
		}
	}

	return scanTargets, err
}
