import "../global.css";
import { RootProvider } from "fumadocs-ui/provider";
import { DocsLayout } from "fumadocs-ui/layouts/docs";
import type { CSSProperties, ReactNode } from "react";
import type { Metadata } from "next";
import { cn } from "@multica/ui/lib/utils";
import { baseOptions } from "@/app/layout.config";
import { source } from "@/lib/source";
import { i18n, type Lang } from "@/lib/i18n";
import { uiTranslations, localeLabels } from "@/lib/translations";
import { DocsSettings } from "@/components/docs-settings";

const fontVariables = {
  "--font-sans":
    'Inter, -apple-system, BlinkMacSystemFont, "Segoe UI", "PingFang SC", "Microsoft YaHei", "Noto Sans CJK SC", sans-serif',
  "--font-mono": '"Geist Mono", ui-monospace, SFMono-Regular, Menlo, Consolas, monospace',
  "--font-serif":
    '"Source Serif 4", ui-serif, "Iowan Old Style", "Apple Garamond", Baskerville, "Times New Roman", serif',
} as CSSProperties;

export const metadata: Metadata = {
  title: {
    template: "%s | Multica Docs",
    default: "Multica Docs",
  },
  description:
    "Documentation for Multica — the open-source managed agents platform.",
};

export function generateStaticParams() {
  return i18n.languages.map((lang) => ({ lang }));
}

export default async function Layout({
  params,
  children,
}: {
  params: Promise<{ lang: string }>;
  children: ReactNode;
}) {
  const { lang: rawLang } = await params;
  const lang = (i18n.languages as readonly string[]).includes(rawLang)
    ? (rawLang as Lang)
    : (i18n.defaultLanguage as Lang);
  const locales = i18n.languages.map((l) => ({
    locale: l,
    name: localeLabels[l],
  }));

  return (
    <html
      lang={lang}
      suppressHydrationWarning
      className={cn("antialiased")}
      style={fontVariables}
    >
      <body className="font-sans">
        <RootProvider
          i18n={{
            locale: lang,
            locales,
            translations: uiTranslations[lang],
          }}
          search={{ options: { api: "/docs/api/search" } }}
        >
          <DocsLayout
            tree={source.getPageTree(lang)}
            // Suppress Fumadocs's default sidebar-footer icons (theme +
            // language + search). Our custom <DocsSettings> is mounted as
            // the sidebar footer instead — two labelled buttons, not three
            // icons.
            themeSwitch={{ enabled: false }}
            searchToggle={{ enabled: false }}
            sidebar={{ footer: <DocsSettings locale={lang} /> }}
            {...baseOptions}
          >
            {children}
          </DocsLayout>
        </RootProvider>
      </body>
    </html>
  );
}
