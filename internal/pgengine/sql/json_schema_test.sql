-- Tests for json_schema.sql, run by pgcov (see .github/workflows/build.yml).
-- One accepting and one rejecting case per schema keyword.

CREATE FUNCTION pg_temp.valid(schema jsonb, data jsonb) RETURNS boolean AS $$
    SELECT timetable.validate_json_schema(schema, data)
$$ LANGUAGE sql;

-- _validate_json_schema_type
DO $$
BEGIN
    ASSERT timetable._validate_json_schema_type('integer', '1');
    ASSERT NOT timetable._validate_json_schema_type('integer', '1.5');
    ASSERT NOT timetable._validate_json_schema_type('integer', '"1"');
    ASSERT timetable._validate_json_schema_type('string', '"x"');
    ASSERT NOT timetable._validate_json_schema_type('string', '1');
END;
$$;

-- type, properties, required
DO $$
BEGIN
    ASSERT pg_temp.valid('{}', '"anything"');
    ASSERT pg_temp.valid('{"type": "number"}', '1.5');
    ASSERT NOT pg_temp.valid('{"type": "number"}', '"1"');
    ASSERT pg_temp.valid('{"type": ["string", "null"]}', 'null');
    ASSERT NOT pg_temp.valid('{"type": ["string", "null"]}', '1');

    ASSERT pg_temp.valid('{"properties": {"a": {"type": "integer"}}}', '{"a": 1}');
    ASSERT pg_temp.valid('{"properties": {"a": {"type": "integer"}}}', '{"b": "x"}'), 'missing property is fine';
    ASSERT NOT pg_temp.valid('{"properties": {"a": {"type": "integer"}}}', '{"a": "x"}');

    ASSERT pg_temp.valid('{"required": ["a"]}', '{"a": 1}');
    ASSERT NOT pg_temp.valid('{"required": ["a", "b"]}', '{"a": 1}');
END;
$$;

-- items, additionalItems, uniqueItems, minItems, maxItems
DO $$
BEGIN
    ASSERT pg_temp.valid('{"items": {"type": "integer"}}', '[1, 2]');
    ASSERT NOT pg_temp.valid('{"items": {"type": "integer"}}', '[1, "2"]');
    ASSERT pg_temp.valid('{"items": [{"type": "integer"}, {"type": "string"}]}', '[1, "a", true]');
    ASSERT NOT pg_temp.valid('{"items": [{"type": "integer"}, {"type": "string"}]}', '["a", 1]');

    ASSERT pg_temp.valid('{"items": [{"type": "integer"}], "additionalItems": false}', '[1]');
    ASSERT NOT pg_temp.valid('{"items": [{"type": "integer"}], "additionalItems": false}', '[1, 2]');
    ASSERT pg_temp.valid('{"items": [{"type": "integer"}], "additionalItems": {"type": "string"}}', '[1, "a"]');
    ASSERT NOT pg_temp.valid('{"items": [{"type": "integer"}], "additionalItems": {"type": "string"}}', '[1, 2]');

    ASSERT pg_temp.valid('{"uniqueItems": true}', '[1, 2]');
    ASSERT NOT pg_temp.valid('{"uniqueItems": true}', '[1, 1]');

    ASSERT pg_temp.valid('{"minItems": 1, "maxItems": 2}', '[1]');
    ASSERT NOT pg_temp.valid('{"minItems": 1}', '[]');
    ASSERT NOT pg_temp.valid('{"maxItems": 1}', '[1, 2]');
END;
$$;

-- numeric keywords
DO $$
BEGIN
    ASSERT pg_temp.valid('{"minimum": 1, "maximum": 3}', '2');
    ASSERT NOT pg_temp.valid('{"minimum": 1}', '0');
    ASSERT NOT pg_temp.valid('{"maximum": 3}', '4');
    ASSERT pg_temp.valid('{"minimum": 1, "exclusiveMinimum": true}', '2');
    ASSERT NOT pg_temp.valid('{"minimum": 1, "exclusiveMinimum": true}', '1');
    ASSERT pg_temp.valid('{"maximum": 3, "exclusiveMaximum": true}', '2');
    ASSERT NOT pg_temp.valid('{"maximum": 3, "exclusiveMaximum": true}', '3');
    ASSERT pg_temp.valid('{"multipleOf": 5}', '10');
    ASSERT NOT pg_temp.valid('{"multipleOf": 5}', '7');
END;
$$;

-- string keywords
DO $$
BEGIN
    ASSERT pg_temp.valid('{"minLength": 1, "maxLength": 3}', '"ab"');
    ASSERT NOT pg_temp.valid('{"minLength": 3}', '"ab"');
    ASSERT NOT pg_temp.valid('{"maxLength": 1}', '"ab"');
    ASSERT pg_temp.valid('{"pattern": "^a"}', '"abc"');
    ASSERT NOT pg_temp.valid('{"pattern": "^a"}', '"xbc"');
    ASSERT pg_temp.valid('{"enum": ["a", 1]}', '1');
    ASSERT NOT pg_temp.valid('{"enum": ["a", 1]}', '2');
END;
$$;

-- object keywords
DO $$
BEGIN
    ASSERT pg_temp.valid('{"minProperties": 1, "maxProperties": 2}', '{"a": 1}');
    ASSERT NOT pg_temp.valid('{"minProperties": 1}', '{}');
    ASSERT NOT pg_temp.valid('{"maxProperties": 1}', '{"a": 1, "b": 2}');

    ASSERT pg_temp.valid('{"properties": {"a": {}}, "additionalProperties": false}', '{"a": 1}');
    ASSERT NOT pg_temp.valid('{"properties": {"a": {}}, "additionalProperties": false}', '{"a": 1, "b": 2}');
    ASSERT pg_temp.valid('{"properties": {"a": {}}, "additionalProperties": {"type": "string"}}', '{"a": 1, "b": "x"}');
    ASSERT NOT pg_temp.valid('{"properties": {"a": {}}, "additionalProperties": {"type": "string"}}', '{"a": 1, "b": 2}');
    ASSERT pg_temp.valid('{"patternProperties": {"^x": {}}, "additionalProperties": false}', '{"x1": 1}'),
        'patternProperties are not additional';

    ASSERT pg_temp.valid('{"patternProperties": {"^n": {"type": "number"}}}', '{"n1": 1, "s": "x"}');
    ASSERT NOT pg_temp.valid('{"patternProperties": {"^n": {"type": "number"}}}', '{"n1": "x"}');

    ASSERT pg_temp.valid('{"dependencies": {"a": ["b"]}}', '{"a": 1, "b": 2}');
    ASSERT pg_temp.valid('{"dependencies": {"a": ["b"]}}', '{"c": 1}'), 'dependency only applies when a is present';
    ASSERT NOT pg_temp.valid('{"dependencies": {"a": ["b"]}}', '{"a": 1}');
    ASSERT pg_temp.valid('{"dependencies": {"a": {"required": ["b"]}}}', '{"a": 1, "b": 2}');
    ASSERT NOT pg_temp.valid('{"dependencies": {"a": {"required": ["b"]}}}', '{"a": 1}');
END;
$$;

-- combinators and $ref
DO $$
BEGIN
    ASSERT pg_temp.valid('{"anyOf": [{"type": "string"}, {"type": "number"}]}', '1');
    ASSERT NOT pg_temp.valid('{"anyOf": [{"type": "string"}, {"type": "number"}]}', 'true');
    ASSERT pg_temp.valid('{"allOf": [{"minimum": 1}, {"maximum": 3}]}', '2');
    ASSERT NOT pg_temp.valid('{"allOf": [{"minimum": 1}, {"maximum": 3}]}', '4');
    ASSERT pg_temp.valid('{"oneOf": [{"minimum": 3}, {"maximum": 1}]}', '4');
    ASSERT NOT pg_temp.valid('{"oneOf": [{"minimum": 1}, {"maximum": 3}]}', '2'), 'matches both';
    ASSERT pg_temp.valid('{"not": {"type": "string"}}', '1');
    ASSERT NOT pg_temp.valid('{"not": {"type": "string"}}', '"x"');

    ASSERT pg_temp.valid('{"definitions": {"pos": {"minimum": 0}}, "properties": {"n": {"$ref": "#/definitions/pos"}}}', '{"n": 1}');
    ASSERT NOT pg_temp.valid('{"definitions": {"pos": {"minimum": 0}}, "properties": {"n": {"$ref": "#/definitions/pos"}}}', '{"n": -1}');
    ASSERT pg_temp.valid('{"definitions": {"a/b": {"type": "string"}}, "$ref": "#/definitions/a~1b"}', '"x"'), 'escaped ref';
END;
$$;
