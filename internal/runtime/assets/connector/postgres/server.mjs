#!/usr/bin/env node
import pg from "pg";
import { Server } from "@modelcontextprotocol/sdk/server/index.js";
import { StdioServerTransport } from "@modelcontextprotocol/sdk/server/stdio.js";
import { CallToolRequestSchema, ListResourcesRequestSchema, ListToolsRequestSchema, ReadResourceRequestSchema } from "@modelcontextprotocol/sdk/types.js";
import { QueryDenied, readOnlySQL, requireReadOnlyRole, roleCheckSQL } from "./policy.mjs";

const MAX_RESULT = 512 * 1024;
const address = process.argv[2];
if (!address) { console.error("缺少数据库连接配置"); process.exit(1); }
const baseURL = new URL(address);
baseURL.protocol = "postgres:";
baseURL.password = "";
const pool = new pg.Pool({ connectionString: address, max: 2, connectionTimeoutMillis: 8000,
  query_timeout: 12000, statement_timeout: 10000, idle_in_transaction_session_timeout: 15000,
  options: "-c default_transaction_read_only=on -c search_path=pg_catalog,public" });
const server = new Server({ name: "aiofcage/postgres-readonly", version: "1.0.0" }, { capabilities: { resources: {}, tools: {} } });

function encodeResult(rows) {
  const text = JSON.stringify(rows, null, 2);
  if (Buffer.byteLength(text) > MAX_RESULT) throw new QueryDenied("查询结果过大，请选择必要字段并缩小 LIMIT");
  return text;
}

async function query(sql, params = []) {
  let client;
  let clean = false;
  try {
    client = await pool.connect();
    await client.query("BEGIN READ ONLY");
    requireReadOnlyRole((await client.query(roleCheckSQL)).rows[0]);
    // 扩展协议拒绝多条语句，外层行数上限限制意外的大结果。
    const result = await client.query({ text: `SELECT * FROM (${sql}) AS aiofcage_result LIMIT 501`, values: params, queryMode: "extended" });
    if (result.rows.length > 500) throw new QueryDenied("查询结果超过 500 行，请添加 LIMIT 或缩小查询范围");
    return encodeResult(result.rows);
  } catch (error) {
    if (error instanceof QueryDenied) throw error;
    throw new QueryDenied(error.code === "28P01" ? "数据库用户名或密码不对" : error.code === "42501" ?
      "数据库账号没有读取该数据的权限" : "数据库查询失败，请检查连接、字段或查询范围");
  } finally {
    if (client) {
      try { await client.query("ROLLBACK"); clean = true; } catch { /* 连接状态不明时销毁，不放回连接池。 */ }
      client.release(!clean);
    }
  }
}

server.setRequestHandler(ListToolsRequestSchema, async () => ({ tools: [{ name: "query",
  description: "查询授权数据库。仅支持单条 SELECT、只读 CTE 与常用内置函数；必须使用专用只读账号。",
  inputSchema: { type: "object", properties: { sql: { type: "string", maxLength: 32768 } }, required: ["sql"], additionalProperties: false },
  annotations: { readOnlyHint: true, destructiveHint: false } }] }));
server.setRequestHandler(CallToolRequestSchema, async ({ params }) => {
  if (params.name !== "query") throw new QueryDenied("工具未授权");
  try {
    const args = params.arguments;
    if (!args || Object.keys(args).some((key) => key !== "sql")) throw new QueryDenied();
    return { content: [{ type: "text", text: await query(readOnlySQL(args.sql)) }], isError: false };
  } catch (error) {
    // 不返回 SQL、连接串、参数或驱动明细，防止数据库配置进入会话与审计。
    const text = error instanceof QueryDenied ? error.message : error.code === "28P01" ? "数据库用户名或密码不对" :
      error.code === "42501" ? "数据库账号没有读取该数据的权限" : "数据库查询失败，请检查连接、字段或查询范围";
    return { content: [{ type: "text", text }], isError: true };
  }
});
server.setRequestHandler(ListResourcesRequestSchema, async () => {
  const rows = JSON.parse(await query("SELECT table_name FROM information_schema.tables WHERE table_schema = 'public' AND table_type = 'BASE TABLE' ORDER BY table_name LIMIT 200"));
  return { resources: rows.map(({ table_name }) => ({ uri: new URL(`${encodeURIComponent(table_name)}/schema`, baseURL).href,
    mimeType: "application/json", name: `${table_name} 表结构` })) };
});
server.setRequestHandler(ReadResourceRequestSchema, async ({ params }) => {
  const resource = new URL(params.uri);
  const parts = resource.pathname.split("/");
  if (parts.pop() !== "schema" || resource.protocol !== baseURL.protocol || resource.host !== baseURL.host || resource.username !== baseURL.username ||
      resource.password || resource.search || resource.hash || parts.length !== 2) throw new QueryDenied("资源未授权");
  const table = decodeURIComponent(parts.pop());
  return { contents: [{ uri: params.uri, mimeType: "application/json", text: await query(
    "SELECT column_name,data_type FROM information_schema.columns WHERE table_schema='public' AND table_name=$1 ORDER BY ordinal_position", [table]) }] };
});
await server.connect(new StdioServerTransport());
process.stdin.once("end", () => { pool.end().catch(() => {}); });
