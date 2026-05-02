import { createReadToolDefinition } from "@mariozechner/pi-coding-agent";
import { Type } from "typebox";
import { spawn } from "child_process";

const GODEEPER_EXTS = new Set([".go", ".py", ".js", ".ts", ".tsx"]);

const readSchema = Type.Object({
	path: Type.String({ description: "Path to the file to read (relative or absolute)" }),
	offset: Type.Optional(Type.Number({ description: "Line number to start reading from (1-indexed)" })),
	limit: Type.Optional(Type.Number({ description: "Maximum number of lines to read" })),
	summary: Type.Optional(
		Type.Boolean({
			description:
				"If true, return a structural summary of the file (symbols, imports, which other files import it) instead of raw content. Ignores offset and limit. Falls back to normal read for unsupported file types.",
		}),
	),
});

const cwd = process.cwd();
const builtinRead = createReadToolDefinition(cwd);

pi.registerTool({
	name: "read",
	label: "read",
	description:
		"Read the contents of a file. Supports text files and images. Set summary=true to get a structural overview (symbols, imports, callers) via godeeper instead of raw content — useful for orientation without reading the full file. summary=true ignores offset and limit, and falls back to normal read for non-code files.",
	parameters: readSchema,
	async execute(toolCallId, params, signal, onUpdate, ctx) {
		const { path, offset, limit, summary } = params;

		const ext = path.substring(path.lastIndexOf(".")).toLowerCase();
		const canSummarize = summary && GODEEPER_EXTS.has(ext);

		if (!canSummarize) {
			return builtinRead.execute(toolCallId, { path, offset, limit }, signal, onUpdate, ctx);
		}

		return new Promise((resolve, reject) => {
			const proc = spawn("godeeper", [path], { cwd });
			let stdout = "";
			let stderr = "";

			proc.stdout.on("data", (d: Buffer) => {
				stdout += d.toString();
			});
			proc.stderr.on("data", (d: Buffer) => {
				stderr += d.toString();
			});

			signal?.addEventListener(
				"abort",
				() => {
					proc.kill();
					reject(new Error("Operation aborted"));
				},
				{ once: true },
			);

			proc.on("close", (code: number | null) => {
				if (code !== 0) {
					reject(new Error(`godeeper failed (exit ${code}): ${stderr}`));
					return;
				}
				resolve({ content: [{ type: "text", text: stdout }], details: {} });
			});
		});
	},
});
