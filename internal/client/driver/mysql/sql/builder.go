package sql

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	umconf "udup/internal/config/mysql"
)

type ValueComparisonSign string

const (
	LessThanComparisonSign            ValueComparisonSign = "<"
	LessThanOrEqualsComparisonSign                        = "<="
	EqualsComparisonSign                                  = "="
	IsEqualsComparisonSign                                = "is"
	GreaterThanOrEqualsComparisonSign                     = ">="
	GreaterThanComparisonSign                             = ">"
	NotEqualsComparisonSign                               = "!="
)

// EscapeName will escape a db/table/column/... name by wrapping with backticks.
// It is not fool proof. I'm just trying to do the right thing here, not solving
// SQL injection issues, which should be irrelevant for this tool.
func EscapeName(name string) string {
	if unquoted, err := strconv.Unquote(name); err == nil {
		name = unquoted
	}
	return fmt.Sprintf("`%s`", name)
}

func EscapeValue(colValue string) string {
	var esc string
	colBuffer := *new(bytes.Buffer)
	last := 0
	for i, c := range colValue {
		switch c {
		case 0:
			esc = `\0`
		case '\n':
			esc = `\n`
		case '\r':
			esc = `\r`
		case '\\':
			esc = `\\`
		case '\'':
			esc = `\'`
		case '"':
			esc = `\"`
		case '\032':
			esc = `\Z`
		default:
			continue
		}
		colBuffer.WriteString(colValue[last:i])
		colBuffer.WriteString(esc)
		last = i + 1
	}
	colBuffer.WriteString(colValue[last:])
	return colBuffer.String()
}

func buildColumnsPreparedValues(columns *umconf.ColumnList) []string {
	values := make([]string, columns.Len(), columns.Len())
	for i, column := range columns.ColumnList() {
		var token string
		if column.TimezoneConversion != nil {
			token = fmt.Sprintf("convert_tz(?, '%s', '%s')", column.TimezoneConversion.ToTimezone, "+00:00")
		} else {
			token = "?"
		}
		values[i] = token
	}
	return values
}

func buildPreparedValues(length int) []string {
	values := make([]string, length, length)
	for i := 0; i < length; i++ {
		values[i] = "?"
	}
	return values
}

func duplicateNames(names []string) []string {
	duplicate := make([]string, len(names), len(names))
	copy(duplicate, names)
	return duplicate
}

func BuildValueComparison(column string, value string, comparisonSign ValueComparisonSign) (result string, err error) {
	if column == "" {
		return "", fmt.Errorf("Empty column in GetValueComparison")
	}
	if value == "" {
		return "", fmt.Errorf("Empty value in GetValueComparison")
	}
	comparison := fmt.Sprintf("(%s %s %s)", EscapeName(column), string(comparisonSign), value)
	return comparison, err
}

func BuildEqualsComparison(columns []string, values []string) (result string, err error) {
	if len(columns) == 0 {
		return "", fmt.Errorf("Got 0 columns in GetEqualsComparison")
	}
	if len(columns) != len(values) {
		return "", fmt.Errorf("Got %d columns but %d values in GetEqualsComparison", len(columns), len(values))
	}
	comparisons := []string{}
	for i, column := range columns {
		value := values[i]
		comparison, err := BuildValueComparison(column, value, EqualsComparisonSign)
		if err != nil {
			return "", err
		}
		comparisons = append(comparisons, comparison)
	}
	result = strings.Join(comparisons, " and ")
	result = fmt.Sprintf("(%s)", result)
	return result, nil
}

func BuildEqualsPreparedComparison(columns []string) (result string, err error) {
	values := buildPreparedValues(len(columns))
	return BuildEqualsComparison(columns, values)
}

func BuildSetPreparedClause(columns *umconf.ColumnList) (result string, err error) {
	if columns.Len() == 0 {
		return "", fmt.Errorf("Got 0 columns in BuildSetPreparedClause")
	}
	setTokens := []string{}
	for _, column := range columns.ColumnList() {
		var setToken string
		if column.TimezoneConversion != nil {
			setToken = fmt.Sprintf("%s=convert_tz(?, '%s', '%s')", EscapeName(column.Name), column.TimezoneConversion.ToTimezone, "+00:00")
		} else {
			setToken = fmt.Sprintf("%s=?", EscapeName(column.Name))
		}
		setTokens = append(setTokens, setToken)
	}
	return strings.Join(setTokens, ", "), nil
}

func BuildRangeComparison(columns []string, values []string, args []interface{}, comparisonSign ValueComparisonSign) (result string, explodedArgs []interface{}, err error) {
	if len(columns) == 0 {
		return "", explodedArgs, fmt.Errorf("Got 0 columns in GetRangeComparison")
	}
	if len(columns) != len(values) {
		return "", explodedArgs, fmt.Errorf("Got %d columns but %d values in GetEqualsComparison", len(columns), len(values))
	}
	if len(columns) != len(args) {
		return "", explodedArgs, fmt.Errorf("Got %d columns but %d args in GetEqualsComparison", len(columns), len(args))
	}
	includeEquals := false
	if comparisonSign == LessThanOrEqualsComparisonSign {
		comparisonSign = LessThanComparisonSign
		includeEquals = true
	}
	if comparisonSign == GreaterThanOrEqualsComparisonSign {
		comparisonSign = GreaterThanComparisonSign
		includeEquals = true
	}
	comparisons := []string{}

	for i, column := range columns {
		//
		value := values[i]
		rangeComparison, err := BuildValueComparison(column, value, comparisonSign)
		if err != nil {
			return "", explodedArgs, err
		}
		if len(columns[0:i]) > 0 {
			equalitiesComparison, err := BuildEqualsComparison(columns[0:i], values[0:i])
			if err != nil {
				return "", explodedArgs, err
			}
			comparison := fmt.Sprintf("(%s AND %s)", equalitiesComparison, rangeComparison)
			comparisons = append(comparisons, comparison)
			explodedArgs = append(explodedArgs, args[0:i]...)
			explodedArgs = append(explodedArgs, args[i])
		} else {
			comparisons = append(comparisons, rangeComparison)
			explodedArgs = append(explodedArgs, args[i])
		}
	}

	if includeEquals {
		comparison, err := BuildEqualsComparison(columns, values)
		if err != nil {
			return "", explodedArgs, nil
		}
		comparisons = append(comparisons, comparison)
		explodedArgs = append(explodedArgs, args...)
	}
	result = strings.Join(comparisons, " or ")
	result = fmt.Sprintf("(%s)", result)
	return result, explodedArgs, nil
}

func BuildDMLDeleteQuery(databaseName, tableName string, tableColumns *umconf.ColumnList, args []*interface{}) (result string, uniqueKeyArgs []interface{}, err error) {
	if len(args) != tableColumns.Len() {
		return result, uniqueKeyArgs, fmt.Errorf("args count differs from table column count in BuildDMLDeleteQuery")
	}
	comparisons := []string{}
	for _, column := range tableColumns.ColumnList() {
		tableOrdinal := tableColumns.Ordinals[column.Name]
		if *args[tableOrdinal] == nil {
			comparison, err := BuildValueComparison(column.Name, "NULL", IsEqualsComparisonSign)
			if err != nil {
				return result, uniqueKeyArgs, err
			}
			comparisons = append(comparisons, comparison)
		} else {
			if strings.HasPrefix(column.ColumnType, "binary"){
				arg := column.ConvertArg(*args[tableOrdinal])
				comparison, err := BuildValueComparison(column.Name, fmt.Sprintf("cast('%v' as %s)", arg, column.ColumnType), EqualsComparisonSign)
				if err != nil {
					return result, uniqueKeyArgs, err
				}
				comparisons = append(comparisons, comparison)
			} else {
				arg := column.ConvertArg(*args[tableOrdinal])
				uniqueKeyArgs = append(uniqueKeyArgs, arg)
				comparison, err := BuildValueComparison(column.Name, "?", EqualsComparisonSign)
				if err != nil {
					return result, uniqueKeyArgs, err
				}
				comparisons = append(comparisons, comparison)
			}
		}
	}
	databaseName = EscapeName(databaseName)
	tableName = EscapeName(tableName)
	if err != nil {
		return result, uniqueKeyArgs, err
	}
	result = fmt.Sprintf(`
			delete
				from
					%s.%s
				where
					%s
		`, databaseName, tableName,
		fmt.Sprintf("(%s)", strings.Join(comparisons, " and ")),
	)
	return result, uniqueKeyArgs, nil
}

func BuildDMLInsertQuery(databaseName, tableName string, tableColumns, sharedColumns, mappedSharedColumns *umconf.ColumnList, args []*interface{}) (result string, sharedArgs []interface{}, err error) {
	if len(args) != tableColumns.Len() {
		return result, sharedArgs, fmt.Errorf("args count differs from table column count in BuildDMLInsertQuery")
	}

	if !sharedColumns.IsSubsetOf(tableColumns) {
		return result, sharedArgs, fmt.Errorf("shared columns is not a subset of table columns in BuildDMLInsertQuery")
	}
	if sharedColumns.Len() == 0 {
		return result, sharedArgs, fmt.Errorf("No shared columns found in BuildDMLInsertQuery")
	}
	databaseName = EscapeName(databaseName)
	tableName = EscapeName(tableName)

	for _, column := range tableColumns.ColumnList() {
		tableOrdinal := tableColumns.Ordinals[column.Name]
		if *args[tableOrdinal] == nil {
			sharedArgs = append(sharedArgs, *args[tableOrdinal])
		} else {
			arg := column.ConvertArg(*args[tableOrdinal])
			sharedArgs = append(sharedArgs, arg)
		}
	}

	mappedSharedColumnNames := duplicateNames(tableColumns.Names())
	for i := range mappedSharedColumnNames {
		mappedSharedColumnNames[i] = EscapeName(mappedSharedColumnNames[i])
	}
	preparedValues := buildColumnsPreparedValues(tableColumns)

	result = fmt.Sprintf(`
			replace into
				%s.%s
					(%s)
				values
					(%s)
		`, databaseName, tableName,
		strings.Join(mappedSharedColumnNames, ", "),
		strings.Join(preparedValues, ", "),
	)
	return result, sharedArgs, nil
}

func BuildDMLUpdateQuery(databaseName, tableName string, tableColumns, sharedColumns, mappedSharedColumns, uniqueKeyColumns *umconf.ColumnList, valueArgs, whereArgs []*interface{}) (result string, sharedArgs, uniqueKeyArgs []interface{}, err error) {
	if len(valueArgs) != tableColumns.Len() {
		return result, sharedArgs, uniqueKeyArgs, fmt.Errorf("value args count differs from table column count in BuildDMLUpdateQuery")
	}
	if len(whereArgs) != tableColumns.Len() {
		return result, sharedArgs, uniqueKeyArgs, fmt.Errorf("where args count differs from table column count in BuildDMLUpdateQuery")
	}
	if !sharedColumns.IsSubsetOf(tableColumns) {
		return result, sharedArgs, uniqueKeyArgs, fmt.Errorf("shared columns is not a subset of table columns in BuildDMLUpdateQuery")
	}
	if sharedColumns.Len() == 0 {
		return result, sharedArgs, uniqueKeyArgs, fmt.Errorf("No shared columns found in BuildDMLUpdateQuery")
	}
	databaseName = EscapeName(databaseName)
	tableName = EscapeName(tableName)

	for _, column := range tableColumns.ColumnList() {
		tableOrdinal := tableColumns.Ordinals[column.Name]
		if *valueArgs[tableOrdinal] == nil || *valueArgs[tableOrdinal] == "NULL" {
			sharedArgs = append(sharedArgs, valueArgs[tableOrdinal])
		} else {
			arg := column.ConvertArg(valueArgs[tableOrdinal])
			sharedArgs = append(sharedArgs, arg)
		}
	}

	comparisons := []string{}
	for _, column := range tableColumns.ColumnList() {
		tableOrdinal := tableColumns.Ordinals[column.Name]
		if *whereArgs[tableOrdinal] == nil {
			comparison, err := BuildValueComparison(column.Name, "NULL", IsEqualsComparisonSign)
			if err != nil {
				return result, sharedArgs, uniqueKeyArgs, err
			}
			comparisons = append(comparisons, comparison)
		} else {
			if strings.HasPrefix(column.ColumnType, "binary"){
				arg := column.ConvertArg(*whereArgs[tableOrdinal])
				comparison, err := BuildValueComparison(column.Name, fmt.Sprintf("cast('%v' as %s)", arg, column.ColumnType), EqualsComparisonSign)
				if err != nil {
					return result, sharedArgs, uniqueKeyArgs, err
				}
				comparisons = append(comparisons, comparison)
			} else {
				arg := column.ConvertArg(*whereArgs[tableOrdinal])
				uniqueKeyArgs = append(uniqueKeyArgs, arg)
				comparison, err := BuildValueComparison(column.Name, "?", EqualsComparisonSign)
				if err != nil {
					return result, sharedArgs, uniqueKeyArgs, err
				}
				comparisons = append(comparisons, comparison)
			}
		}
	}

	setClause, err := BuildSetPreparedClause(mappedSharedColumns)

	result = fmt.Sprintf(`
 			update
 					%s.%s
				set
					%s
				where
 					%s
 				limit 1
 		`, databaseName, tableName,
		setClause,
		fmt.Sprintf("(%s)", strings.Join(comparisons, " and ")),
	)
	return result, sharedArgs, uniqueKeyArgs, nil
}
