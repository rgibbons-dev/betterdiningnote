import { createSignal } from "solid-js";
import { api, User } from "./api";

interface Props {
  onLogin: (user: User) => void;
}

export default function Auth(props: Props) {
  const [isRegister, setIsRegister] = createSignal(false);
  const [username, setUsername] = createSignal("");
  const [password, setPassword] = createSignal("");
  const [error, setError] = createSignal("");
  const [submitting, setSubmitting] = createSignal(false);

  const handleSubmit = async (e: Event) => {
    e.preventDefault();
    setError("");
    setSubmitting(true);
    try {
      const fn = isRegister() ? api.register : api.login;
      const user = await fn(username(), password());
      props.onLogin(user);
    } catch (err: any) {
      setError(err.message);
    }
    setSubmitting(false);
  };

  return (
    <div class="auth-page">
      <div class="auth-card">
        <h1 class="auth-title">Meal Tracker</h1>
        <p class="auth-subtitle">
          {isRegister() ? "Create an account" : "Sign in to your account"}
        </p>

        {error() && <div class="error-msg">{error()}</div>}

        <form onSubmit={handleSubmit}>
          <div class="field">
            <label for="username">Username</label>
            <input
              id="username"
              type="text"
              value={username()}
              onInput={(e) => setUsername(e.currentTarget.value)}
              placeholder="Enter username"
              minLength={3}
              maxLength={64}
              required
              autocomplete="username"
            />
          </div>
          <div class="field">
            <label for="password">Password</label>
            <input
              id="password"
              type="password"
              value={password()}
              onInput={(e) => setPassword(e.currentTarget.value)}
              placeholder="Enter password"
              minLength={8}
              maxLength={128}
              required
              autocomplete={isRegister() ? "new-password" : "current-password"}
            />
          </div>
          <button class="btn btn-primary btn-full" type="submit" disabled={submitting()}>
            {submitting()
              ? "..."
              : isRegister()
                ? "Create account"
                : "Sign in"}
          </button>
        </form>

        <p class="auth-toggle">
          {isRegister() ? "Already have an account?" : "Don't have an account?"}{" "}
          <button
            class="link-btn"
            onClick={() => {
              setIsRegister(!isRegister());
              setError("");
            }}
          >
            {isRegister() ? "Sign in" : "Create one"}
          </button>
        </p>
      </div>
    </div>
  );
}
