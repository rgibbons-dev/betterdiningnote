import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, fireEvent, screen } from "@solidjs/testing-library";
import Auth from "./Auth";

// Mock the api module
vi.mock("./api", () => ({
  api: {
    register: vi.fn(),
    login: vi.fn(),
  },
}));

import { api } from "./api";

const mockLogin = api.login as ReturnType<typeof vi.fn>;
const mockRegister = api.register as ReturnType<typeof vi.fn>;

beforeEach(() => {
  vi.clearAllMocks();
});

describe("Auth component", () => {
  it("renders sign in form by default", () => {
    const onLogin = vi.fn();
    render(() => <Auth onLogin={onLogin} />);

    expect(screen.getByText("Sign in to your account")).toBeTruthy();
    expect(screen.getByLabelText("Username")).toBeTruthy();
    expect(screen.getByLabelText("Password")).toBeTruthy();
    expect(screen.getByText("Sign in")).toBeTruthy();
  });

  it("toggles to register mode", async () => {
    const onLogin = vi.fn();
    render(() => <Auth onLogin={onLogin} />);

    const toggleBtn = screen.getByText("Create one");
    await fireEvent.click(toggleBtn);

    expect(screen.getByText("Create an account")).toBeTruthy();
    expect(screen.getByText("Create account")).toBeTruthy();
  });

  it("toggles back to login mode", async () => {
    const onLogin = vi.fn();
    render(() => <Auth onLogin={onLogin} />);

    await fireEvent.click(screen.getByText("Create one"));
    await fireEvent.click(screen.getByText("Sign in"));

    expect(screen.getByText("Sign in to your account")).toBeTruthy();
  });

  it("calls api.login on submit in login mode", async () => {
    const onLogin = vi.fn();
    mockLogin.mockResolvedValueOnce({ id: 1, username: "alice" });

    render(() => <Auth onLogin={onLogin} />);

    const usernameInput = screen.getByLabelText("Username");
    const passwordInput = screen.getByLabelText("Password");

    await fireEvent.input(usernameInput, { target: { value: "alice" } });
    await fireEvent.input(passwordInput, { target: { value: "password123" } });
    await fireEvent.submit(screen.getByText("Sign in").closest("form")!);

    // Wait for async handler
    await vi.waitFor(() => {
      expect(mockLogin).toHaveBeenCalledWith("alice", "password123");
      expect(onLogin).toHaveBeenCalledWith({ id: 1, username: "alice" });
    });
  });

  it("calls api.register on submit in register mode", async () => {
    const onLogin = vi.fn();
    mockRegister.mockResolvedValueOnce({ id: 2, username: "bob" });

    render(() => <Auth onLogin={onLogin} />);

    await fireEvent.click(screen.getByText("Create one"));

    const usernameInput = screen.getByLabelText("Username");
    const passwordInput = screen.getByLabelText("Password");

    await fireEvent.input(usernameInput, { target: { value: "bob" } });
    await fireEvent.input(passwordInput, { target: { value: "securepass" } });
    await fireEvent.submit(screen.getByText("Create account").closest("form")!);

    await vi.waitFor(() => {
      expect(mockRegister).toHaveBeenCalledWith("bob", "securepass");
      expect(onLogin).toHaveBeenCalledWith({ id: 2, username: "bob" });
    });
  });

  it("displays error message on login failure", async () => {
    const onLogin = vi.fn();
    mockLogin.mockRejectedValueOnce(new Error("invalid username or password"));

    render(() => <Auth onLogin={onLogin} />);

    const usernameInput = screen.getByLabelText("Username");
    const passwordInput = screen.getByLabelText("Password");

    await fireEvent.input(usernameInput, { target: { value: "alice" } });
    await fireEvent.input(passwordInput, { target: { value: "wrong" } });
    await fireEvent.submit(screen.getByText("Sign in").closest("form")!);

    await vi.waitFor(() => {
      expect(screen.getByText("invalid username or password")).toBeTruthy();
    });
    expect(onLogin).not.toHaveBeenCalled();
  });

  it("clears error when toggling between modes", async () => {
    const onLogin = vi.fn();
    mockLogin.mockRejectedValueOnce(new Error("some error"));

    render(() => <Auth onLogin={onLogin} />);

    await fireEvent.input(screen.getByLabelText("Username"), {
      target: { value: "a" },
    });
    await fireEvent.input(screen.getByLabelText("Password"), {
      target: { value: "b" },
    });
    await fireEvent.submit(screen.getByText("Sign in").closest("form")!);

    await vi.waitFor(() => {
      expect(screen.getByText("some error")).toBeTruthy();
    });

    await fireEvent.click(screen.getByText("Create one"));
    expect(screen.queryByText("some error")).toBeNull();
  });
});
