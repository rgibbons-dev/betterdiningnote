import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@solidjs/testing-library";

// Mock api
vi.mock("./api", () => ({
  api: {
    me: vi.fn(),
    logout: vi.fn(),
    getMealTypes: vi.fn(),
    getMeals: vi.fn(),
  },
}));

import { api } from "./api";
import App from "./App";

const mockMe = api.me as ReturnType<typeof vi.fn>;
const mockGetMealTypes = api.getMealTypes as ReturnType<typeof vi.fn>;
const mockGetMeals = api.getMeals as ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
  // Default: getMealTypes and getMeals return empty arrays (needed by Tracker)
  mockGetMealTypes.mockResolvedValue([]);
  mockGetMeals.mockResolvedValue([]);
});

describe("App component", () => {
  it("shows loading state initially", () => {
    // Never resolve me() so we stay in loading
    mockMe.mockReturnValue(new Promise(() => {}));
    render(() => <App />);
    expect(screen.getByText("Loading...")).toBeTruthy();
  });

  it("shows auth form when not logged in", async () => {
    mockMe.mockRejectedValueOnce(new Error("not authenticated"));

    render(() => <App />);

    await vi.waitFor(() => {
      expect(screen.getByText("Sign in to your account")).toBeTruthy();
    });
  });

  it("shows tracker when logged in", async () => {
    mockMe.mockResolvedValueOnce({ id: 1, username: "testuser" });

    render(() => <App />);

    await vi.waitFor(() => {
      expect(screen.getByText("Meal Tracker")).toBeTruthy();
      expect(screen.getByText("testuser")).toBeTruthy();
      expect(screen.getByText("Log out")).toBeTruthy();
    });
  });
});
