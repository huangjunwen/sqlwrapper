package mysql

import (
	"context"
	"database/sql"
	"fmt"
	"reflect"

	"github.com/go-sql-driver/mysql"

	"github.com/huangjunwen/sqlw/dbcontext"
)

type mysqlDrv struct{}

var (
	_ dbcontext.Drv            = mysqlDrv{}
	_ dbcontext.DrvWithAutoInc = mysqlDrv{}
)

var (
	// DataTypes is the full list of data type in mysql drv
	DataTypes = []string{
		// Float types
		"float32",
		"float64",
		// Int types
		"bool",
		"int8",
		"uint8",
		"int16",
		"uint16",
		"int32",
		"uint32",
		"int64",
		"uint64",
		// Time types
		"time",
		// String types
		"bit",
		"json",
		"string",
	}
)

var (
	// Copy from github.com/go-sql-driver/mysql/fields.go
	scanTypeFloat32   = reflect.TypeOf(float32(0))
	scanTypeFloat64   = reflect.TypeOf(float64(0))
	scanTypeInt8      = reflect.TypeOf(int8(0))
	scanTypeInt16     = reflect.TypeOf(int16(0))
	scanTypeInt32     = reflect.TypeOf(int32(0))
	scanTypeInt64     = reflect.TypeOf(int64(0))
	scanTypeNullFloat = reflect.TypeOf(sql.NullFloat64{})
	scanTypeNullInt   = reflect.TypeOf(sql.NullInt64{})
	scanTypeNullTime  = reflect.TypeOf(mysql.NullTime{})
	scanTypeUint8     = reflect.TypeOf(uint8(0))
	scanTypeUint16    = reflect.TypeOf(uint16(0))
	scanTypeUint32    = reflect.TypeOf(uint32(0))
	scanTypeUint64    = reflect.TypeOf(uint64(0))
	scanTypeRawBytes  = reflect.TypeOf(sql.RawBytes{})
	scanTypeUnknown   = reflect.TypeOf(new(interface{}))
)

const (
	// Copy from github.com/go-sql-driver/mysql/const.go
	flagUnsigned = 1 << 5
)

func (drv mysqlDrv) ExtractQueryResultColumns(conn *sql.Conn, query string) (columns []dbcontext.Col, err error) {
	rows, err := conn.QueryContext(context.Background(), query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	columnTypes, err := rows.ColumnTypes()
	if err != nil {
		return nil, err
	}

	for i, columnType := range columnTypes {

		// XXX: From current driver's public API some information is lost:
		// - Column type's length is not support yet (see https://github.com/go-sql-driver/mysql/pull/667)
		// - Unsigned or not can't be determine when scan type is NullInt64
		// Do some tricks to read them from private fields.
		//
		// NOTE: In general reading data from private field is not a good idea
		field := reflect.
			ValueOf(rows).          // *sql.Rows
			Elem().                 // sql.Rows
			FieldByName("rowsi").   // driver.Rows
			Elem().                 // *mysql.mysqlRows
			Elem().                 // mysql.mysqlRows
			FieldByName("rs").      // mysql.resultSet
			FieldByName("columns"). // []mysql.mysqlField
			Index(i)                // mysql.mysqlField

		length := field.FieldByName("length").Uint()
		flags := field.FieldByName("flags").Uint()
		isUnsigned := (flags & flagUnsigned) != 0

		databaseTypeName := columnType.DatabaseTypeName()
		scanType := columnType.ScanType()

		dataType := ""

		bad := func() {
			panic(fmt.Errorf("Unknown column type: scantype=%#v databaseTypeName=%+q", scanType, databaseTypeName))
		}

		switch scanType {
		// Float types
		case scanTypeFloat32:
			dataType = "float32"
		case scanTypeFloat64:
			dataType = "float64"
		case scanTypeNullFloat:
			switch databaseTypeName {
			case "FLOAT":
				dataType = "float32"
			case "DOUBLE":
				dataType = "float64"
			default:
				bad()
			}

		// Int types, includeing bool type
		case scanTypeInt8:
			if length == 1 {
				// Special case for bool
				dataType = "bool"
			} else {
				dataType = "int8"
			}
		case scanTypeInt16:
			dataType = "int16"
		case scanTypeInt32:
			dataType = "int32"
		case scanTypeInt64:
			dataType = "int64"
		case scanTypeUint8:
			dataType = "uint8"
		case scanTypeUint16:
			dataType = "uint16"
		case scanTypeUint32:
			dataType = "uint32"
		case scanTypeUint64:
			dataType = "uint64"
		case scanTypeNullInt:
			switch databaseTypeName {
			case "TINYINT":
				if isUnsigned {
					dataType = "uint8"
				} else {
					if length == 1 {
						dataType = "bool"
					} else {
						dataType = "int8"
					}
				}
			case "SMALLINT", "YEAR":
				if isUnsigned {
					dataType = "uint16"
				} else {
					dataType = "int16"
				}
			case "MEDIUMINT", "INT":
				if isUnsigned {
					dataType = "uint32"
				} else {
					dataType = "int32"
				}
			case "BIGINT":
				if isUnsigned {
					dataType = "uint64"
				} else {
					dataType = "int64"
				}
			default:
				bad()
			}

		// Time types
		case scanTypeNullTime:
			dataType = "time"

			// String types
		case scanTypeRawBytes:
			switch databaseTypeName {
			case "BIT":
				dataType = "bit"
			case "JSON":
				dataType = "json"
			default:
				dataType = "string"
			}

		default:
			bad()
		}

		columns = append(columns, dbcontext.Col{
			ColumnType: columnType,
			DataType:   dataType,
		})

	}

	return

}

func (drv mysqlDrv) ExtractTableNames(conn *sql.Conn) (tableNames []string, err error) {
	dbName, err := extractDBName(conn)
	if err != nil {
		return nil, err
	}

	rows, err := conn.QueryContext(context.Background(), `
	SELECT 
		TABLE_NAME
	FROM
		INFORMATION_SCHEMA.TABLES
	WHERE
		TABLE_SCHEMA=? AND TABLE_TYPE='BASE TABLE'
	`, dbName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		tableName := ""
		if err = rows.Scan(&tableName); err != nil {
			return nil, err
		}
		tableNames = append(tableNames, tableName)
	}
	return tableNames, nil
}

func (drv mysqlDrv) ExtractColumns(conn *sql.Conn, tableName string) (columns []dbcontext.Col, err error) {
	return drv.ExtractQueryResultColumns(conn, "SELECT * FROM `"+tableName+"`")
}

func (drv mysqlDrv) ExtractAutoIncColumn(conn *sql.Conn, tableName string) (columnName string, err error) {
	dbName, err := extractDBName(conn)
	if err != nil {
		return "", err
	}

	rows, err := conn.QueryContext(context.Background(), `
	SELECT
		COLUMN_NAME
	FROM
		INFORMATION_SCHEMA.COLUMNS
	WHERE
		TABLE_SCHEMA=? AND TABLE_NAME=? AND EXTRA LIKE ?
	`, dbName, tableName, "%auto_increment%")
	if err != nil {
		return "", err
	}
	defer rows.Close()

	for rows.Next() {
		if err := rows.Scan(&columnName); err != nil {
			return "", err
		}
		break
	}

	return columnName, nil
}

func (drv mysqlDrv) ExtractIndexNames(conn *sql.Conn, tableName string) (indexNames []string, err error) {
	dbName, err := extractDBName(conn)
	if err != nil {
		return nil, err
	}

	rows, err := conn.QueryContext(context.Background(), `
	SELECT 
		DISTINCT INDEX_NAME 
	FROM 
		INFORMATION_SCHEMA.STATISTICS 
	WHERE 
		TABLE_SCHEMA=? AND TABLE_NAME=?`, dbName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		indexName := ""
		if err = rows.Scan(&indexName); err != nil {
			return nil, err
		}
		indexNames = append(indexNames, indexName)
	}
	return indexNames, nil

}

func (drv mysqlDrv) ExtractIndex(conn *sql.Conn, tableName string, indexName string) (columnNames []string, isPrimary bool, isUnique bool, err error) {
	dbName, err := extractDBName(conn)
	if err != nil {
		return nil, false, false, err
	}

	rows, err := conn.QueryContext(context.Background(), `
	SELECT 
		NON_UNIQUE, COLUMN_NAME, SEQ_IN_INDEX 
	FROM
		INFORMATION_SCHEMA.STATISTICS
	WHERE
		TABLE_SCHEMA=? AND TABLE_NAME=? AND INDEX_NAME=?
	ORDER BY SEQ_IN_INDEX`, dbName, tableName, indexName)
	if err != nil {
		return nil, false, false, err
	}
	defer rows.Close()

	nonUnique := true
	prevSeq := 0
	for rows.Next() {
		columnName := ""
		seq := 0
		if err := rows.Scan(&nonUnique, &columnName, &seq); err != nil {
			return nil, false, false, err
		}

		// Check seq.
		if seq != prevSeq+1 {
			panic(fmt.Errorf("Bad SEQ_IN_INDEX, prev is %d, current is %d", prevSeq, seq))
		}
		prevSeq = seq

		columnNames = append(columnNames, columnName)
	}

	if len(columnNames) == 0 {
		return nil, false, false, fmt.Errorf("Index %+q in table %+q not found", indexName, tableName)
	}

	// https://dev.mysql.com/doc/refman/5.7/en/create-table.html
	// The name of a PRIMARY KEY is always PRIMARY, which thus cannot be used as the name for any other kind of index
	isPrimary = indexName == "PRIMARY"
	isUnique = !nonUnique
	return
}

func (drv mysqlDrv) ExtractFKNames(conn *sql.Conn, tableName string) (fkNames []string, err error) {
	dbName, err := extractDBName(conn)
	if err != nil {
		return nil, err
	}

	rows, err := conn.QueryContext(context.Background(), `
	SELECT
		CONSTRAINT_NAME
	FROM
		INFORMATION_SCHEMA.TABLE_CONSTRAINTS
	WHERE
		TABLE_SCHEMA=? AND TABLE_NAME = ? AND CONSTRAINT_TYPE = 'FOREIGN KEY'`, dbName, tableName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	for rows.Next() {
		fkName := ""
		if err := rows.Scan(&fkName); err != nil {
			return nil, err
		}
		fkNames = append(fkNames, fkName)
	}
	return fkNames, nil
}

func (drv mysqlDrv) ExtractFK(conn *sql.Conn, tableName string, fkName string) (columnNames []string, refTableName string, refColumnNames []string, err error) {
	dbName, err := extractDBName(conn)
	if err != nil {
		return nil, "", nil, err
	}

	rows, err := conn.QueryContext(context.Background(), `
		SELECT
			COLUMN_NAME, ORDINAL_POSITION, REFERENCED_TABLE_NAME, REFERENCED_COLUMN_NAME
		FROM
			INFORMATION_SCHEMA.KEY_COLUMN_USAGE
		WHERE
			TABLE_SCHEMA=? AND TABLE_NAME=? AND CONSTRAINT_NAME=? ORDER BY ORDINAL_POSITION`, dbName, tableName, fkName)
	if err != nil {
		return nil, "", nil, err
	}
	defer rows.Close()

	prevPos := 0
	for rows.Next() {
		columnName := ""
		refColumnName := ""
		pos := 0
		if err := rows.Scan(&columnName, &pos, &refTableName, &refColumnName); err != nil {
			return nil, "", nil, err
		}

		// Check pos.
		if pos != prevPos+1 {
			panic(fmt.Errorf("Bad ORDINAL_POSITION, prev is %d, current is %d", prevPos, pos))
		}
		prevPos = pos

		columnNames = append(columnNames, columnName)
		refColumnNames = append(refColumnNames, refColumnName)
	}

	if len(columnNames) == 0 {
		return nil, "", nil, fmt.Errorf("FK %+q in table %+q not found", fkName, tableName)
	}

	return columnNames, refTableName, refColumnNames, nil

}

func (drv mysqlDrv) DataTypes() []string {
	return DataTypes
}

func (drv mysqlDrv) Quote(identifier string) string {
	return fmt.Sprintf("`%s`", identifier)
}

func extractDBName(conn *sql.Conn) (string, error) {
	var dbName sql.NullString
	// NOTE: https://dev.mysql.com/doc/refman/5.7/en/information-functions.html#function_database
	// SELECT DATABASE() returns current database or NULL if there is no current default database.
	err := conn.QueryRowContext(context.Background(), "SELECT DATABASE()").Scan(&dbName)
	if err != nil {
		return "", err
	}
	if dbName.String == "" {
		return "", fmt.Errorf("No db selected")
	}
	return dbName.String, nil
}

func init() {
	dbcontext.RegistDrv("mysql", mysqlDrv{})
}
