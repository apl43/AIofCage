import test from "node:test";
import assert from "node:assert/strict";
import { readOnlySQL, requireReadOnlyRole } from "./policy.mjs";

test("保留查表、统计、联表、只读 CTE 和字面量，重建 SQL 而不执行原文", () => {
  for (const sql of [
    "SELECT id,name FROM public.items WHERE id > 0 ORDER BY id LIMIT 20",
    "SELECT a.id,count(*) AS n FROM items a LEFT JOIN orders b ON a.id=b.id GROUP BY a.id",
    "WITH x AS (SELECT 1 AS n) SELECT * FROM x UNION ALL SELECT 2",
    "SELECT coalesce(NULL,1), pg_catalog.round(12.3,1), current_user, now()",
    "SELECT 'COMMIT; COPY x TO PROGRAM; pg_read_file' AS text",
    "SELECT CASE WHEN id IN (1,2) THEN upper(name) ELSE 'other' END FROM items",
    "SELECT 1::int, date '2026-10-25', sum(id) OVER () FROM items",
  ]) assert.ok(readOnlySQL(sql), sql);
  const rebuilt = readOnlySQL("SELECT count(*) FROM items -- harmless comment");
  assert.match(rebuilt, /pg_catalog/);
  assert.ok(!rebuilt.includes("comment"));
});

test("拒绝会话中实际出现的数据库文件读取与程序执行路径", () => {
  for (const sql of [
    "SELECT pg_read_file('/etc/hostname')", "SELECT * FROM pg_ls_dir('/')",
    "COPY (SELECT 'noop-poc') TO PROGRAM 'true'", "COPY items TO '/tmp/out'",
    "SELECT pg_catalog.pg_read_binary_file('/etc/passwd')", "SELECT lo_import('/etc/passwd')",
    "SELECT dblink('host=example.com','select 1')", "SELECT pg_terminate_backend(1)",
    "SELECT set_config('transaction_read_only','off',false)",
    "SELECT public.count(*) FROM items", "SELECT public.lower('x')",
    "SELECT 'x'::public.dangerous", "SELECT 'x'::regprocedure",
  ]) assert.throws(() => readOnlySQL(sql), /只读|允许|安全/, sql);
});

test("拒绝事务拆分、大小写、注释、嵌套写入与未识别语法", () => {
  for (const sql of [
    "COMMIT; DELETE FROM items", "SELECT 1; COMMIT; SELECT 2", "SELECT 1;/**/SET ROLE admin",
    "WITH x AS (DELETE FROM items RETURNING *) SELECT * FROM x",
    "WITH x AS (INSERT INTO items VALUES(1) RETURNING *) SELECT * FROM x",
    "SELECT * FROM items FOR UPDATE", "SELECT * INTO x FROM items", "CALL evil()",
    "sElEcT \"pg_read_file\"('/etc/passwd')", "SELECT $1", "SELECT E'\\x00'::public.evil",
    "SELECT 1\0", "SELECT " + "(1+".repeat(100) + "1" + ")".repeat(100),
  ]) assert.throws(() => readOnlySQL(sql), undefined, sql.slice(0,100));
});

test("账号权限校验缺失、管理角色或写权限均拒绝", () => {
  assert.doesNotThrow(() => requireReadOnlyRole({ unsafe: false }));
  for (const row of [null, {}, { unsafe: true }, { unsafe: "false" }]) assert.throws(() => requireReadOnlyRole(row), /权限过高/);
});
