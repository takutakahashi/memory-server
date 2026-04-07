import { getPage } from '@/lib/api';
import MarkdownContent from '@/components/MarkdownContent';
import Link from 'next/link';
import { notFound } from 'next/navigation';
import type { Metadata } from 'next';

interface PageDetailProps {
  params: Promise<{ id: string }>;
}

export async function generateMetadata({ params }: PageDetailProps): Promise<Metadata> {
  const { id } = await params;
  try {
    const page = await getPage(id);
    return {
      title: `${page.title} | 社内 Wiki`,
      description: page.summary || page.content.slice(0, 120),
    };
  } catch {
    return { title: '記事が見つかりません | 社内 Wiki' };
  }
}

export default async function PageDetailPage({ params }: PageDetailProps) {
  const { id } = await params;

  let page;
  try {
    page = await getPage(id);
  } catch (e) {
    const msg = e instanceof Error ? e.message : '';
    if (msg.includes('404') || msg.toLowerCase().includes('not found')) {
      notFound();
    }
    return (
      <div className="mx-auto max-w-4xl px-4 sm:px-6 py-10">
        <div className="rounded-xl border border-red-200 bg-red-50 p-6 text-red-700">
          <p className="font-medium">エラーが発生しました</p>
          <p className="text-sm mt-1">{msg || '記事の取得に失敗しました'}</p>
        </div>
      </div>
    );
  }

  const createdAt = new Date(page.created_at).toLocaleDateString('ja-JP', {
    year: 'numeric', month: 'long', day: 'numeric',
  });
  const updatedAt = new Date(page.updated_at).toLocaleDateString('ja-JP', {
    year: 'numeric', month: 'long', day: 'numeric',
  });

  return (
    <div className="mx-auto max-w-4xl px-4 sm:px-6 py-10">
      {/* Breadcrumb */}
      <nav className="mb-6 flex items-center gap-2 text-sm text-slate-400">
        <Link href="/" className="hover:text-indigo-600 transition-colors">ホーム</Link>
        <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <Link href="/pages" className="hover:text-indigo-600 transition-colors">記事一覧</Link>
        {page.category && (
          <>
            <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
              <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
            </svg>
            <Link
              href={`/pages?category=${encodeURIComponent(page.category)}`}
              className="hover:text-indigo-600 transition-colors"
            >
              {page.category}
            </Link>
          </>
        )}
        <svg xmlns="http://www.w3.org/2000/svg" className="h-3.5 w-3.5" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M9 5l7 7-7 7" />
        </svg>
        <span className="text-slate-600 truncate max-w-xs">{page.title}</span>
      </nav>

      <article className="rounded-2xl border border-slate-200 bg-white shadow-sm overflow-hidden">
        {/* Article header */}
        <div className="border-b border-slate-100 bg-slate-50 px-6 sm:px-8 py-6">
          {page.category && (
            <div className="mb-3">
              <Link
                href={`/pages?category=${encodeURIComponent(page.category)}`}
                className="inline-block rounded bg-amber-50 px-2.5 py-1 text-xs font-semibold text-amber-700 ring-1 ring-inset ring-amber-200 hover:bg-amber-100 transition-colors"
              >
                {page.category}
              </Link>
            </div>
          )}
          <h1 className="text-2xl sm:text-3xl font-bold text-slate-900 leading-snug">
            {page.title}
          </h1>
          {page.summary && (
            <p className="mt-3 text-slate-500 leading-relaxed">{page.summary}</p>
          )}

          {/* Meta */}
          <div className="mt-4 flex flex-wrap items-center gap-4 text-xs text-slate-400">
            <span>作成: {createdAt}</span>
            {updatedAt !== createdAt && <span>更新: {updatedAt}</span>}
            <span>v{page.version}</span>
            <span className={`inline-flex items-center gap-1 rounded-full px-2 py-0.5 font-medium ${
              page.scope === 'public'
                ? 'bg-green-50 text-green-600 ring-1 ring-green-200'
                : 'bg-slate-100 text-slate-500 ring-1 ring-slate-200'
            }`}>
              {page.scope === 'public' ? '公開' : '非公開'}
            </span>
          </div>

          {/* Tags */}
          {page.tags && page.tags.length > 0 && (
            <div className="mt-3 flex flex-wrap gap-1.5">
              {page.tags.map((tag) => (
                <Link
                  key={tag}
                  href={`/search?q=${encodeURIComponent(tag)}`}
                  className="inline-block rounded-full bg-indigo-50 px-2.5 py-0.5 text-xs font-medium text-indigo-600 ring-1 ring-inset ring-indigo-200 hover:bg-indigo-100 transition-colors"
                >
                  #{tag}
                </Link>
              ))}
            </div>
          )}
        </div>

        {/* Article body */}
        <div className="px-6 sm:px-8 py-8">
          {page.content ? (
            <MarkdownContent content={page.content} />
          ) : (
            <p className="text-slate-400 italic">内容がありません</p>
          )}
        </div>
      </article>

      {/* Back link */}
      <div className="mt-8">
        <Link
          href="/pages"
          className="inline-flex items-center gap-2 text-sm font-medium text-indigo-600 hover:underline"
        >
          <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
          </svg>
          記事一覧に戻る
        </Link>
      </div>
    </div>
  );
}
