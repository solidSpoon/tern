/**
 * Tern's Pi extension.
 *
 * It gives the model two tools and nothing else:
 *  - publish_article reads article.json from the work directory and hands it to
 *    the Tern process, which validates it, localizes its pictures, writes it to
 *    the library and warms the dictionary;
 *  - upload_image stores a picture in Tern's media folder.
 *
 * Both call back over HTTP: validation and storage stay in one process, so
 * there is one schema and one writer.
 */
import * as fs from "node:fs";
import * as path from "node:path";
import { defineTool, type ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Type } from "typebox";

const publishUrl = process.env.TERN_PUBLISH_URL ?? "";
const imageUrl = process.env.TERN_IMAGE_URL ?? "";

const publish = defineTool({
	name: "publish_article",
	label: "Publish Article",
	description:
		"Hand the article.json in the working directory to Tern: it checks the structure, downloads the pictures, writes the article to the library and warms the dictionary. It takes no arguments; every call reads article.json again and updates the same article.",
	promptGuidelines: [
		"publish_article takes no arguments: it always reads article.json in the working directory.",
		"After changing article.json, call it again. Every call replaces the same article.",
		"Call it once article.json is written and you have checked that the paragraph count matches the source.",
		"Only \"已入库\" means the article landed. Never finish without it.",
	],
	parameters: Type.Object({}),
	async execute() {
		const file = path.resolve(process.cwd(), "article.json");
		if (!fs.existsSync(file)) {
			return {
				content: [
					{ type: "text", text: "No " + file + ". Write the article JSON into the working directory first." },
				],
				details: {},
				isError: true,
			};
		}

		let raw: unknown;
		try {
			raw = JSON.parse(fs.readFileSync(file, "utf8"));
		} catch (error) {
			return {
				content: [
					{ type: "text", text: "That file is not valid JSON (" + (error as Error).message + "): " + file },
				],
				details: {},
				isError: true,
			};
		}

		if (!publishUrl) {
			return {
				content: [{ type: "text", text: "TERN_PUBLISH_URL is not set, so Tern cannot take the article." }],
				details: {},
				isError: true,
			};
		}

		try {
			const response = await fetch(publishUrl, {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify(raw),
			});
			const data = (await response.json()) as {
				ok?: boolean;
				articleId?: string;
				blocks?: number;
				issues?: string[];
				error?: string;
			};

			if (data?.ok) {
				return {
					content: [
						{
							type: "text",
							text:
								"已入库：" +
								data.articleId +
								"（" + data.blocks + " 段）",
						},
					],
					details: data,
				};
			}

			const issues = (data?.issues ?? []).join("\n") || data?.error || "unknown error";
			return {
				content: [
					{
						type: "text",
						text:
							"The structure did not pass. Fix " +
							path.basename(file) +
							" and call publish_article again:\n" +
							issues,
					},
				],
				details: data,
				isError: true,
			};
		} catch (error) {
			return {
				content: [{ type: "text", text: "Calling Tern failed: " + (error as Error).message }],
				details: {},
				isError: true,
			};
		}
	},
});

const uploadImage = defineTool({
	name: "upload_image",
	label: "Upload Image",
	description:
		"Store a picture in Tern's local media folder and get its /media/... address, ready for the cover or an image block of article.json.",
	promptGuidelines: [
		"Images usually keep their original src: Tern downloads them when the article is imported. Upload only pictures you worked on yourself (replaced, cropped, converted).",
		"Write the returned /media/... into the article JSON unchanged.",
	],
	parameters: Type.Object({
		path: Type.Optional(Type.String({ description: "A picture in the working directory, such as images/cover.jpg" })),
		url: Type.Optional(Type.String({ description: "A remote picture for Tern to download" })),
		label: Type.Optional(Type.String({ description: "A file name prefix, such as cover" })),
	}),
	async execute(_toolCallId, params) {
		if (!params?.path && !params?.url) {
			return {
				content: [{ type: "text", text: "Give either path (a local file) or url (a remote picture)." }],
				details: {},
				isError: true,
			};
		}
		if (!imageUrl) {
			return {
				content: [{ type: "text", text: "TERN_IMAGE_URL is not set, so Tern cannot take the picture." }],
				details: {},
				isError: true,
			};
		}

		let body: Record<string, unknown>;
		if (params.path) {
			const file = path.resolve(process.cwd(), params.path);
			if (!fs.existsSync(file)) {
				return {
					content: [{ type: "text", text: "No such file: " + file }],
					details: {},
					isError: true,
				};
			}
			const label = params.label || path.basename(file).replace(/\.[a-z0-9]+$/i, "") || "img";
			body = { dataBase64: fs.readFileSync(file).toString("base64"), label };
		} else {
			body = { url: params.url, label: params.label };
		}

		try {
			const response = await fetch(imageUrl, {
				method: "POST",
				headers: { "content-type": "application/json" },
				body: JSON.stringify(body),
			});
			const data = (await response.json()) as {
				ok?: boolean;
				src?: string;
				bytes?: number;
				ext?: string;
				error?: string;
			};
			if (data?.ok && data.src) {
				return {
					content: [
						{
							type: "text",
							text:
								"Uploaded: " +
								data.src +
								" (" +
								Math.round((data.bytes ?? 0) / 1024) +
								" KB, " +
								data.ext +
								"). Write it into article.json as it is.",
						},
					],
					details: data,
				};
			}
			return {
				content: [{ type: "text", text: "Upload failed: " + (data?.error ?? "unknown error") }],
				details: data,
				isError: true,
			};
		} catch (error) {
			return {
				content: [{ type: "text", text: "Upload failed: " + (error as Error).message }],
				details: {},
				isError: true,
			};
		}
	},
});

export default function (pi: ExtensionAPI) {
	pi.registerTool(publish);
	pi.registerTool(uploadImage);
}
