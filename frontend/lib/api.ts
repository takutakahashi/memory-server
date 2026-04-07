const API_BASE_URL = process.env.NEXT_PUBLIC_API_BASE_URL || 'http://localhost:8080';
const API_TOKEN = process.env.NEXT_PUBLIC_API_TOKEN || '';
const DEFAULT_USER_ID = process.env.NEXT_PUBLIC_DEFAULT_USER_ID || 'default';

export interface KBPage {
  page_id: string;
  user_id: string;
  title: string;
  slug: string;
  content: string;
  summary: string;
  category: string;
  scope: 'private' | 'public';
  tags: string[];
  source_memory_ids: string[];
  version: number;
  created_at: string;
  updated_at: string;
}

export interface ListResult {
  pages: KBPage[];
  next_token?: string;
}

export interface SearchResult {
  page: KBPage;
  score: number;
}

function getHeaders(): HeadersInit {
  const headers: HeadersInit = {
    'Content-Type': 'application/json',
  };
  if (API_TOKEN) {
    headers['Authorization'] = `Bearer ${API_TOKEN}`;
  }
  return headers;
}

export async function listPages(params?: {
  category?: string;
  limit?: number;
  next_token?: string;
  user_id?: string;
}): Promise<ListResult> {
  const query = new URLSearchParams();
  query.set('user_id', params?.user_id || DEFAULT_USER_ID);
  if (params?.category) query.set('category', params.category);
  if (params?.limit) query.set('limit', String(params.limit));
  if (params?.next_token) query.set('next_token', params.next_token);

  const res = await fetch(`${API_BASE_URL}/api/v1/kb?${query}`, {
    headers: getHeaders(),
    next: { revalidate: 30 },
  });
  if (!res.ok) throw new Error(`Failed to list pages: ${res.statusText}`);
  return res.json();
}

export async function getPage(id: string): Promise<KBPage> {
  const res = await fetch(`${API_BASE_URL}/api/v1/kb/${id}`, {
    headers: getHeaders(),
    next: { revalidate: 30 },
  });
  if (!res.ok) throw new Error(`Failed to get page: ${res.statusText}`);
  return res.json();
}

export async function getPageBySlug(slug: string): Promise<KBPage> {
  const res = await fetch(`${API_BASE_URL}/api/v1/kb/slug/${slug}`, {
    headers: getHeaders(),
    next: { revalidate: 30 },
  });
  if (!res.ok) throw new Error(`Failed to get page: ${res.statusText}`);
  return res.json();
}

export async function searchPages(params: {
  query: string;
  limit?: number;
  user_id?: string;
}): Promise<SearchResult[]> {
  const res = await fetch(`${API_BASE_URL}/api/v1/kb/search`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify({
      user_id: params.user_id || DEFAULT_USER_ID,
      query: params.query,
      limit: params.limit || 20,
    }),
  });
  if (!res.ok) throw new Error(`Failed to search pages: ${res.statusText}`);
  return res.json();
}

export async function createPage(data: {
  title: string;
  slug: string;
  content: string;
  summary?: string;
  category?: string;
  scope?: 'private' | 'public';
  tags?: string[];
  user_id?: string;
}): Promise<KBPage> {
  const res = await fetch(`${API_BASE_URL}/api/v1/kb`, {
    method: 'POST',
    headers: getHeaders(),
    body: JSON.stringify({ ...data, user_id: data.user_id || DEFAULT_USER_ID }),
  });
  if (!res.ok) throw new Error(`Failed to create page: ${res.statusText}`);
  return res.json();
}

export async function updatePage(
  id: string,
  data: {
    title?: string;
    slug?: string;
    content?: string;
    summary?: string;
    category?: string;
    scope?: 'private' | 'public';
    tags?: string[];
  }
): Promise<KBPage> {
  const res = await fetch(`${API_BASE_URL}/api/v1/kb/${id}`, {
    method: 'PUT',
    headers: getHeaders(),
    body: JSON.stringify(data),
  });
  if (!res.ok) throw new Error(`Failed to update page: ${res.statusText}`);
  return res.json();
}

export async function deletePage(id: string): Promise<void> {
  const res = await fetch(`${API_BASE_URL}/api/v1/kb/${id}`, {
    method: 'DELETE',
    headers: getHeaders(),
  });
  if (!res.ok) throw new Error(`Failed to delete page: ${res.statusText}`);
}
