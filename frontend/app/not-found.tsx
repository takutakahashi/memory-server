import Link from 'next/link';

export default function NotFound() {
  return (
    <div className="mx-auto max-w-4xl px-4 py-20 text-center">
      <p className="text-6xl font-bold text-indigo-200">404</p>
      <h2 className="mt-4 text-2xl font-bold text-slate-800">ページが見つかりません</h2>
      <p className="mt-2 text-slate-500">お探しのページは存在しないか、削除された可能性があります。</p>
      <div className="mt-8 flex justify-center gap-4">
        <Link
          href="/"
          className="rounded-lg bg-indigo-600 px-5 py-2.5 text-sm font-semibold text-white hover:bg-indigo-700 transition-colors"
        >
          ホームへ戻る
        </Link>
        <Link
          href="/pages"
          className="rounded-lg border border-slate-200 px-5 py-2.5 text-sm font-semibold text-slate-700 hover:border-indigo-300 transition-colors"
        >
          記事一覧へ
        </Link>
      </div>
    </div>
  );
}
