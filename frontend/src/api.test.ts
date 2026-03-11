import { describe, it, expect, vi, beforeEach } from "vitest";

// Mock fetch globally
const mockFetch = vi.fn();
vi.stubGlobal("fetch", mockFetch);

// Import after mocking
import { api } from "./api";

function jsonResponse(data: unknown, status = 200) {
  return new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function errorResponse(error: string, status: number) {
  return new Response(JSON.stringify({ error }), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

beforeEach(() => {
  mockFetch.mockReset();
});

describe("api.register", () => {
  it("sends POST with credentials and returns user", async () => {
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ id: 1, username: "alice" })
    );

    const user = await api.register("alice", "password123");

    expect(user).toEqual({ id: 1, username: "alice" });
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/auth/register",
      expect.objectContaining({
        method: "POST",
        credentials: "include",
        body: JSON.stringify({ username: "alice", password: "password123" }),
      })
    );
  });

  it("throws on error response", async () => {
    mockFetch.mockResolvedValueOnce(
      errorResponse("registration failed", 400)
    );

    await expect(api.register("alice", "pw")).rejects.toThrow(
      "registration failed"
    );
  });
});

describe("api.login", () => {
  it("sends POST and returns user", async () => {
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ id: 1, username: "alice" })
    );

    const user = await api.login("alice", "password123");
    expect(user).toEqual({ id: 1, username: "alice" });
  });
});

describe("api.logout", () => {
  it("sends POST to logout", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ status: "ok" }));

    await api.logout();
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/auth/logout",
      expect.objectContaining({ method: "POST" })
    );
  });
});

describe("api.me", () => {
  it("sends GET and returns user", async () => {
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ id: 1, username: "alice" })
    );

    const user = await api.me();
    expect(user).toEqual({ id: 1, username: "alice" });
  });

  it("throws when not authenticated", async () => {
    mockFetch.mockResolvedValueOnce(
      errorResponse("not authenticated", 401)
    );

    await expect(api.me()).rejects.toThrow("not authenticated");
  });
});

describe("api.getMealTypes", () => {
  it("returns meal types array", async () => {
    const types = [
      { id: 1, name: "Breakfast", sort_order: 0 },
      { id: 2, name: "Lunch", sort_order: 1 },
    ];
    mockFetch.mockResolvedValueOnce(jsonResponse(types));

    const result = await api.getMealTypes();
    expect(result).toEqual(types);
  });
});

describe("api.addMealType", () => {
  it("sends POST with name", async () => {
    mockFetch.mockResolvedValueOnce(
      jsonResponse({ id: 5, name: "Brunch", sort_order: 4 })
    );

    const result = await api.addMealType("Brunch");
    expect(result.name).toBe("Brunch");
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/meal-types",
      expect.objectContaining({
        body: JSON.stringify({ name: "Brunch" }),
      })
    );
  });
});

describe("api.deleteMealType", () => {
  it("sends DELETE with encoded name", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ status: "ok" }));

    await api.deleteMealType("Second Breakfast");
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/meal-types/Second%20Breakfast",
      expect.objectContaining({ method: "DELETE" })
    );
  });
});

describe("api.getMeals", () => {
  it("fetches meals for date", async () => {
    const meals = [
      { date: "2025-03-01", meal_name: "Breakfast", content: "Eggs", sort_order: 0 },
    ];
    mockFetch.mockResolvedValueOnce(jsonResponse(meals));

    const result = await api.getMeals("2025-03-01");
    expect(result).toEqual(meals);
    expect(mockFetch).toHaveBeenCalledWith(
      "/api/meals?date=2025-03-01",
      expect.objectContaining({ method: "GET" })
    );
  });
});

describe("api.saveMeals", () => {
  it("sends PUT with date and meals", async () => {
    mockFetch.mockResolvedValueOnce(jsonResponse({ status: "ok" }));

    const meals = [
      { date: "2025-03-01", meal_name: "Breakfast", content: "Toast", sort_order: 0 },
    ];
    await api.saveMeals("2025-03-01", meals);

    expect(mockFetch).toHaveBeenCalledWith(
      "/api/meals",
      expect.objectContaining({
        method: "PUT",
        body: JSON.stringify({ date: "2025-03-01", meals }),
      })
    );
  });
});

describe("api.exportUrl", () => {
  it("returns correct URL for csv", () => {
    expect(api.exportUrl("csv")).toBe("/api/export?format=csv");
  });

  it("returns correct URL for xlsx", () => {
    expect(api.exportUrl("xlsx")).toBe("/api/export?format=xlsx");
  });

  it("returns correct URL for ods", () => {
    expect(api.exportUrl("ods")).toBe("/api/export?format=ods");
  });
});

describe("request error handling", () => {
  it("uses statusText when error JSON parsing fails", async () => {
    mockFetch.mockResolvedValueOnce(
      new Response("not json", {
        status: 500,
        statusText: "Internal Server Error",
      })
    );

    await expect(api.me()).rejects.toThrow("Internal Server Error");
  });
});
