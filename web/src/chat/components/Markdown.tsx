import { useMemo } from 'react';
import { marked } from 'marked';
import DOMPurify from 'dompurify';

marked.setOptions({ gfm: true, breaks: true });

DOMPurify.addHook('afterSanitizeAttributes', node => {
  if (node.tagName === 'A') {
    node.setAttribute('target', '_blank');
    node.setAttribute('rel', 'noreferrer noopener');
  }
});

/** Assistant message body: markdown → sanitized HTML. User messages stay plain text. */
export function Markdown({ content, className }: { content: string; className?: string }) {
  const html = useMemo(() => {
    const raw = marked.parse(content || '', { async: false });
    return DOMPurify.sanitize(raw, { USE_PROFILES: { html: true } });
  }, [content]);
  return <div className={className} dangerouslySetInnerHTML={{ __html: html }} />;
}
