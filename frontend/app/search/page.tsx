import { searchPages, SearchResult } from '@/lib/api';
import Link from 'next/link';
import { Suspense } from 'react';

interface SearchPageProps {
  searchParams: Promise<{ q?: string }>;
}

async function SearchResults({ query }: { query: string }) {
  if (!query.trim()) {
    return (
      <div className="rounded-xl border border-dashed border-slate-300 bg-white p-12 text-center text-slate-400">
        <p>検索キーワードを入力してください</p>
      </div>
    );
  }

  let results: SearchResult[] = [];
  let error: string | null = null;

  try {
    results = await searchPages({ query, limit: 20 });
  } catch (e) {
    error = e instanceof Error ? e.message : '検索に失敗しました';
  }

  if (error) {
    return (
      <div className="rounded-xl border border-red-200 bg-red-50 p-6 text-red-700">
        <p className="font-medium">エラーが発生しました</p>
        <p className="text-sm mt-1">{error}</p>
      </div>
    );
  }

  if (results.length === 0) {
    return (
      <div className="rounded-xl border border-dashed border-slate-300 bg-white p-12 text-center text-slate-400">
        <svg xmlns="http://www.w3.org/2000/svg" className="mx-auto h-12 w-12 mb-3 text-slate-300" fill="none" viewBox="0 0 24 24" stroke="currentColor">
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={1.5} d="M21 21l-6-6m2-5a7 7 0 11-14 0 7 7 0 0114 0z" />
        </svg>
        <p className="font-medium">「{query}」に一致する記事が見つかりませんでした</p>
        <p className="text-sm mt-1">別のキーワードで試してみてください</p>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {results.map(({ page, score }) => {
        const updatedAt = new Date(page.updated_at).toLocaleDateString('ja-JP', {
          year: 'numeric', month: 'short', day: 'numeric',
        });

        return (
          <Link
            key={page.page_id}
            href={`/pages/${page.page_id}`}
            className="group flex flex-col gap-2 rounded-xl border border-slate-200 bg-white p-5 shadow-sm hover:shadow-md hover:border-indigo-300 transition-all"
          >
            <div className="flex items-start justify-between gap-3">
              <h3 className="font-semibold text-slate-900 group-hover:text-indigo-600 transition-colors leading-snug">
                {page.title}
              </h3>
              <div className="flex items-center gap-2 flex-shrink-0">
                {page.category && (
                  <span className="rounded bg-amber-50 px-2 py-0.5 text-xs font-semibold text-amber-700 ring-1 ring-inset ring-amber-200">
                    {page.category}
                  </span>
                )}
                <span className="text-xs text-slate-400 bg-slate-100 rounded-full px-2 py-0.5 font-mono">
                  {(score * 100).toFixed(0)}%
                </span>
              </div>
            </div>
            {page.summary && (
              <p className="text-sm text-slate-500 line-clamp-2 leading-relaxed">{page.summary}</p>
            )}
            <div className="flex items-center justify-between text-xs text-slate-400 pt-1">
              <div className="flex flex-wrap gap-1">
                {page.tags?.slice(0, 3).map((tag) => (
                  <span key={tag} className="bg-indigo-50 text-indigo-600 rounded-full px-2 py-0.5 font-medium ring-1 ring-inset ring-indigo-200">
                    #{tag}
                  </span>
                ))}
              </div>
              <span>更新: {updatedAt}</span>
            </div>
          </Link>
        );
      })}
    </div>
  );
}

export default async function SearchPage({ searchParams }: SearchPageProps) {
  const { q } = await searchParams;
  const query = q || '';

  return (
    <div className="mx-auto max-w-4xl px-4 sm:px-6 py-10 space-y-8">
      {/* Header */}
      <div>
        <h1 className="text-2xl font-bold text-slate-900">
          {query ? `「${query}」の検索結果` : '検索'}
        </h1>
        {query && (
          <p className="text-sm text-slate-500 mt-1">
            Knowledge Base からセマンティック検索しています
          </p>
        )}
      </div>

      {/* Search form */}
      <form method="get" action="/search" className="flex gap-2">
        <input
          type="search"
          name="q"
          defaultValue={query}
          placeholder="キーワードで記事を検索..."
          className="flex-1 rounded-lg border border-slate-300 bg-white px-4 py-2.5 text-sm text-slate-800 focus:outline-none focus:ring-2 focus:ring-indigo-500"
        />
        <button
          type="submit"
          className="rounded-lg bg-indigo-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-indigo-700 transition-colors"
        >
          検索
        </button>
      </form>

      {/* Results */}
      <Suspense
        fallback={
          <div className="space-y-4">
            {[...Array(4)].map((_, i) => (
              <div key={i} className="h-28 rounded-xl border border-slate-200 bg-white animate-pulse" />
            ))}
          </div>
        }
      >
        <SearchResults query={query} />
      </Suspense>

      {/* Back link */}
      <div>
        <Link
          href="/pages"
          className="inline-flex items-center gap-2 text-sm font-medium text-indigo-600 hover:underline"
        >
          <svg xmlns="http://www.w3.org/2000/svg" className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor">
            <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M15 19l-7-7 7-7" />
          </svg>
          記事一覧へ
        </Link>
      </div>
    </div>
  );
}
