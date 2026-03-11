import { createSignal, onMount, Show } from "solid-js";
import { api, User } from "./api";
import Auth from "./Auth";
import Tracker from "./Tracker";
import "./styles.css";

export default function App() {
  const [user, setUser] = createSignal<User | null>(null);
  const [loading, setLoading] = createSignal(true);

  onMount(async () => {
    try {
      const u = await api.me();
      setUser(u);
    } catch {
      // not logged in
    }
    setLoading(false);
  });

  const handleLogout = async () => {
    await api.logout();
    setUser(null);
  };

  return (
    <div class="app">
      <Show when={!loading()} fallback={<div class="loading">Loading...</div>}>
        <Show
          when={user()}
          fallback={<Auth onLogin={(u) => setUser(u)} />}
        >
          {(u) => (
            <>
              <header class="app-header">
                <h1>Meal Tracker</h1>
                <div class="header-right">
                  <span class="username">{u().username}</span>
                  <button class="btn btn-ghost" onClick={handleLogout}>
                    Log out
                  </button>
                </div>
              </header>
              <Tracker />
            </>
          )}
        </Show>
      </Show>
    </div>
  );
}
