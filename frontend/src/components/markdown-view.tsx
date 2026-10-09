import type { Element, Root, RootContent } from "hast";
import ReactMarkdown from "react-markdown";
import rehypeRaw from "rehype-raw";
import rehypeSanitize, { defaultSchema } from "rehype-sanitize";
import remarkGfm from "remark-gfm";

import { assetUrl } from "@/lib/api";

// Converted posts keep media that Markdown has no syntax for (video, audio)
// as raw html. Allow those elements, and nothing that can run code.
const schema = {
  ...defaultSchema,
  tagNames: [...(defaultSchema.tagNames ?? []), "video", "audio", "source", "picture", "figure", "figcaption"],
  attributes: {
    ...defaultSchema.attributes,
    video: ["src", "poster", "controls", "loop", "muted", "playsInline", "preload", "width", "height"],
    audio: ["src", "controls", "loop", "muted", "preload"],
    source: ["src", "srcSet", "type", "media"],
    img: [...(defaultSchema.attributes?.img ?? []), "srcSet", "width", "height", "loading"],
  },
};

const mediaProps = ["src", "poster", "href", "srcSet"] as const;

/** Points the post's relative assets/ references at the API. */
function rehypeAssets(id: number) {
  const rewrite = (value: string) =>
    value
      .split(",")
      .map((part) => part.replace(/^(\s*)(assets\/\S+)/, (_, space: string, path: string) => space + assetUrl(id, path)))
      .join(",");
  const visit = (node: Root | RootContent) => {
    if (node.type === "element") {
      const el = node as Element;
      for (const key of mediaProps) {
        const value = el.properties[key];
        if (typeof value === "string") el.properties[key] = rewrite(value);
      }
    }
    if ("children" in node) node.children.forEach(visit);
  };
  return () => (tree: Root) => visit(tree);
}

export function MarkdownView({ id, markdown }: { id: number; markdown: string }) {
  return (
    <article className="prose prose-neutral max-w-none dark:prose-invert prose-pre:bg-secondary prose-pre:text-foreground prose-img:rounded-lg prose-video:rounded-lg prose-a:break-words prose-table:block prose-table:overflow-x-auto">
      <ReactMarkdown
        remarkPlugins={[remarkGfm]}
        rehypePlugins={[rehypeRaw, [rehypeSanitize, schema], rehypeAssets(id)]}
        components={{
          a: ({ node: _, ...props }) => <a {...props} target="_blank" rel="noreferrer" />,
          video: ({ node: _, ...props }) => <video {...props} className="w-full" />,
          // Without the page's styles, icons drawn as SVG would fill the width.
          img: ({ node: _, ...props }) => (
            <img
              {...props}
              loading="lazy"
              className={props.src?.endsWith(".svg") ? "max-h-40 w-auto" : "max-h-[75vh] w-auto"}
            />
          ),
        }}
      >
        {markdown}
      </ReactMarkdown>
    </article>
  );
}
