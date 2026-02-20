const BASE = "";

async function request<T>(
  method: string,
  path: string,
  body?: unknown
): Promise<T> {
  const opts: RequestInit = {
    method,
    credentials: "include",
    headers: { "Content-Type": "application/json" },
  };
  if (body !== undefined) {
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(BASE + path, opts);
  if (!res.ok) {
    const data = await res.json().catch(() => ({ error: res.statusText }));
    throw new Error(data.error || res.statusText);
  }
  // For file downloads, return the response itself
  if (
    res.headers.get("Content-Type")?.includes("text/csv") ||
    res.headers.get("Content-Type")?.includes("spreadsheet") ||
    res.headers.get("Content-Type")?.includes("opendocument")
  ) {
    return res as unknown as T;
  }
  return res.json();
}

export interface User {
  id: number;
  username: string;
}

export interface MealType {
  id: number;
  name: string;
  sort_order: number;
}

export interface Meal {
  id?: number;
  date: string;
  meal_name: string;
  content: string;
  sort_order: number;
}

export const api = {
  // Auth
  register: (username: string, password: string) =>
    request<User>("POST", "/api/auth/register", { username, password }),
  login: (username: string, password: string) =>
    request<User>("POST", "/api/auth/login", { username, password }),
  logout: () => request<void>("POST", "/api/auth/logout"),
  me: () => request<User>("GET", "/api/auth/me"),

  // Meal types
  getMealTypes: () => request<MealType[]>("GET", "/api/meal-types"),
  addMealType: (name: string) =>
    request<MealType>("POST", "/api/meal-types", { name }),
  deleteMealType: (name: string) =>
    request<void>("DELETE", `/api/meal-types/${encodeURIComponent(name)}`),

  // Meals
  getMeals: (date: string) => request<Meal[]>("GET", `/api/meals?date=${date}`),
  saveMeals: (date: string, meals: Meal[]) =>
    request<void>("PUT", "/api/meals", { date, meals }),

  // Export
  exportUrl: (format: string) => `/api/export?format=${format}`,
};
