import React from 'react';
import ReactMarkdown from 'react-markdown';
import remarkGfm from 'remark-gfm';
import { remarkSoftBreaks } from './markdownSoftBreaks';

/**
 * The assistant's prose, rendered as the markdown it is written in.
 *
 * The model replies in markdown — bold run names, bulleted lists of needs,
 * `REQ-12` in backticks, the occasional table — and until this existed the
 * chat printed every asterisk and backtick literally. A reply that reads
 * "- **PER-1 Chief Engineer** → NEED-2" is harder to follow than the list it
 * was meant to be, and it makes the assistant look broken rather than
 * verbose.
 *
 * Raw HTML is deliberately NOT enabled. react-markdown escapes HTML unless
 * rehype-raw is added, and this text comes from a language model, so anything
 * that walked out of the model as `<img onerror=…>` must stay text. Markdown
 * formatting is worth having; an HTML injection point in the middle of the
 * app is not.
 *
 * The same soft-break plugin the rest of the app uses applies here: a single
 * newline is a line break, because the assistant lays out steps and lists on
 * their own lines and folding them into a paragraph loses the structure.
 */

interface ChatMarkdownProps {
  text: string;
  /**
   * A reply still arriving. Draws the caret at the end of the last line
   * through CSS, so it sits after the final word rather than dropping onto a
   * line of its own the way an element after a block would.
   */
  streaming?: boolean;
}

export const ChatMarkdown: React.FC<ChatMarkdownProps> = ({ text, streaming }) => (
  <div className={`markdown-content chat-markdown${streaming ? ' chat-markdown--streaming' : ''}`}>
    <ReactMarkdown
      remarkPlugins={[remarkGfm, remarkSoftBreaks]}
      components={{
        // A link the assistant offers opens in a new tab, so following it
        // never throws away the conversation behind it.
        a: ({ href, children, ...rest }) => (
          <a href={href} target="_blank" rel="noopener noreferrer" {...rest}>
            {children}
          </a>
        ),
      }}
    >
      {text}
    </ReactMarkdown>
  </div>
);
