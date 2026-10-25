import { parse, toSql } from "pgsql-ast-parser";

const statements = new Set(["select", "with", "union", "union all", "values"]);
const nodes = new Set([...statements, "table", "statement", "ref", "integer", "numeric", "string", "boolean", "null",
  "binary", "unary", "ternary", "cast", "constant", "call", "keyword", "case", "list", "array", "array select",
  "arrayIndex", "member", "extract", "substring", "overlay", "INNER JOIN", "LEFT JOIN", "RIGHT JOIN", "FULL JOIN", "CROSS JOIN"]);
const functions = new Set(("count sum avg min max string_agg array_agg json_agg jsonb_agg json_object_agg jsonb_object_agg " +
  "bool_and bool_or every lower upper length char_length octet_length trim btrim ltrim rtrim substring substr " +
  "replace split_part concat concat_ws left right strpos position abs round ceil ceiling floor trunc mod power sqrt " +
  "now date_trunc date_part to_char to_date to_timestamp age current_database current_schema current_schemas " +
  "version row_number rank dense_rank percent_rank cume_dist ntile lag lead first_value last_value nth_value " +
  "json_build_object jsonb_build_object json_build_array jsonb_build_array json_array_length jsonb_array_length").split(" "));
// 这几项是 SQL 语法构造，不能加 pg_catalog 前缀。
const constructs = new Set(["coalesce", "nullif", "greatest", "least"]);
// 类型名称可能含空格，单独列出，避免把未知类型当作安全的转换函数。
const safeTypes = new Set(["bool", "boolean", "int", "int2", "int4", "int8", "integer", "smallint", "bigint", "real", "float4", "float8",
  "double precision", "decimal", "numeric", "text", "varchar", "character varying", "char", "character", "bpchar", "date", "time", "timetz",
  "timestamp", "timestamptz", "time without time zone", "time with time zone", "timestamp without time zone", "timestamp with time zone",
  "interval", "json", "jsonb", "uuid", "bytea"]);

export class QueryDenied extends Error {
  constructor(message = "只读查询仅支持单条 SELECT，不允许写入、事务控制或未授权函数") { super(message); }
}

function checkType(type) {
  if (!type || (type.schema && type.schema !== "pg_catalog") || !safeTypes.has(type.name?.toLowerCase())) throw new QueryDenied();
}

// 默认拒绝未识别的语法；执行重新生成的 SQL，绝不把原始字符串交给数据库。
export function readOnlySQL(sql) {
  if (typeof sql !== "string" || !sql.trim() || Buffer.byteLength(sql) > 32768 || sql.includes("\0")) throw new QueryDenied();
  let tree;
  try { tree = parse(sql); } catch { throw new QueryDenied("无法确认 SQL 为安全只读查询，请使用单条 SELECT"); }
  if (tree.length !== 1 || !statements.has(tree[0].type)) throw new QueryDenied();
  let count = 0;
  function walk(value, depth = 0) {
    if (!value || typeof value !== "object") return;
    if (++count > 4000 || depth > 80) throw new QueryDenied("查询结构过于复杂，请缩小查询范围");
    if (Array.isArray(value)) { value.forEach((item) => walk(item, depth + 1)); return; }
    if (value.type && !nodes.has(value.type)) throw new QueryDenied();
    if (value.type === "call") {
      const name = value.function.name.toLowerCase();
      if (value.function.schema && value.function.schema !== "pg_catalog") throw new QueryDenied();
      if (constructs.has(name) && !value.function.schema) value.function.name = name;
      else {
        if (!functions.has(name)) throw new QueryDenied("该函数未列入只读工具允许范围");
        value.function = { name, schema: "pg_catalog" };
      }
    }
    if (value.type === "cast") checkType(value.to);
    if (value.type === "constant") checkType(value.dataType);
    if (value.opSchema && value.opSchema !== "pg_catalog") throw new QueryDenied();
    for (const child of Object.values(value)) walk(child, depth + 1);
  }
  walk(tree);
  try { return toSql.statement(tree[0]); } catch { throw new QueryDenied(); }
}

// 除查询形态外，还核验账号的有效权限，包含可 SET ROLE 的间接成员关系。
export const roleCheckSQL = `SELECT
  EXISTS (SELECT 1 FROM pg_catalog.pg_roles r WHERE
    (r.rolsuper OR r.rolcreaterole OR r.rolcreatedb OR r.rolreplication OR r.rolbypassrls OR
     r.rolname IN ('pg_read_server_files','pg_write_server_files','pg_execute_server_program','pg_signal_backend','pg_checkpoint','pg_maintain'))
    AND pg_catalog.pg_has_role(current_user, r.oid, 'MEMBER'))
  OR pg_catalog.has_database_privilege(current_user, current_database(), 'CREATE')
  OR EXISTS (SELECT 1 FROM pg_catalog.pg_namespace n WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema'
    AND pg_catalog.has_schema_privilege(current_user,n.oid,'CREATE'))
  OR EXISTS (SELECT 1 FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid=c.relnamespace
    WHERE n.nspname !~ '^pg_' AND n.nspname <> 'information_schema' AND c.relkind IN ('r','p','v','m','f')
    AND pg_catalog.has_table_privilege(current_user,c.oid,'INSERT,UPDATE,DELETE,TRUNCATE,REFERENCES,TRIGGER')) AS unsafe`;

export function requireReadOnlyRole(row) {
  if (row?.unsafe !== false) throw new QueryDenied("数据库账号权限过高：请改用专用只读账号，撤销管理角色、建库建表和数据写入权限");
}
