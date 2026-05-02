import { createReadToolDefinition } from "@mariozechner/pi-coding-agent";
import type { ExtensionAPI } from "@mariozechner/pi-coding-agent";
import { Type } from "typebox";
import { spawn } from "child_process";

export default function (pi: ExtensionAPI) {
	const GODEEPER_EXTS = new Set([".go", ".py", ".js", ".ts", ".tsx"]);

	const readSchema = Type.Object({
		path: Type.String({ description: "Path to the file to read (relative or absolute)" }),
		offset: Type.Optional(Type.Number({ description: "Line number to start reading from (1-indexed)" })),
		limit: Type.Optional(Type.Number({ description: "Maximum number of lines to read" })),
		full_contents: Type.Optional(
			Type.Boolean({
				description:
					"Set to true to read the raw file contents. By default (false), supported code files (.go, .py, .js, .ts, .tsx) return a structural summary (symbols, imports, callers) instead. offset and limit are only used when full_contents=true.",
			}),
		),
	});

	const cwd = process.cwd();
	const builtinRead = createReadToolDefinition(cwd);

	pi.registerTool({
		name: "read",
		label: "read",
		description:
			"Read a file. For code files (.go, .py, .js, .ts, .tsx), returns a structural summary (symbols, imports, callers) by default. Set full_contents=true to read raw file contents instead.",
		promptSnippet: "Read a file. Returns a structural summary for code files by default; set full_contents=true for raw contents.",
		promptGuidelines: [
			"Use read to examine files instead of cat or sed.",
			"For code files (.go, .py, .js, .ts, .tsx), read returns a structural summary by default — only set full_contents=true when you need the raw source.",
		],
		parameters: readSchema,
		async execute(toolCallId, params, signal, onUpdate, ctx) {
			const { path, offset, limit, full_contents } = params;

			const ext = path.substring(path.lastIndexOf(".")).toLowerCase();
			const useSummary = !full_contents && GODEEPER_EXTS.has(ext);

			if (!useSummary) {
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
}