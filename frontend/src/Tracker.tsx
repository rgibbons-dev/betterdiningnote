import {
  createSignal,
  createEffect,
  onMount,
  For,
  Show,
  batch,
} from "solid-js";
import { createStore, produce } from "solid-js/store";
import { api, Meal, MealType } from "./api";

function formatDate(d: Date): string {
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

function parseDate(s: string): Date {
  const [y, m, d] = s.split("-").map(Number);
  return new Date(y, m - 1, d);
}

function displayDate(s: string): string {
  const d = parseDate(s);
  return d.toLocaleDateString("en-US", {
    weekday: "short",
    year: "numeric",
    month: "short",
    day: "numeric",
  });
}

interface MealEntry {
  meal_name: string;
  content: string;
  sort_order: number;
}

// Local cache type: date -> meals
type DayCache = Record<string, MealEntry[]>;
// Track what's been saved (from server) per date
type SavedState = Record<string, MealEntry[]>;

export default function Tracker() {
  const [selectedDate, setSelectedDate] = createSignal(formatDate(new Date()));
  const [mealTypes, setMealTypes] = createSignal<MealType[]>([]);
  const [cache, setCache] = createStore<DayCache>({});
  const [saved, setSaved] = createStore<SavedState>({});
  const [saving, setSaving] = createSignal(false);
  const [newMealName, setNewMealName] = createSignal("");
  const [bannerOpen, setBannerOpen] = createSignal(true);
  const [dateInput, setDateInput] = createSignal("");

  // Load meal types on mount
  onMount(async () => {
    const types = await api.getMealTypes();
    setMealTypes(types);
  });

  // Load meals when date changes
  createEffect(async () => {
    const date = selectedDate();
    setDateInput(date);
    if (cache[date]) return; // Already cached

    const meals = await api.getMeals(date);
    const types = mealTypes();

    // Build entry list from meal types, merging with server data
    const entries: MealEntry[] = types.map((t, i) => {
      const existing = meals.find((m) => m.meal_name === t.name);
      return {
        meal_name: t.name,
        content: existing?.content ?? "",
        sort_order: existing?.sort_order ?? i,
      };
    });

    // Add any server meals not in current types
    for (const m of meals) {
      if (!entries.find((e) => e.meal_name === m.meal_name)) {
        entries.push({
          meal_name: m.meal_name,
          content: m.content,
          sort_order: m.sort_order,
        });
      }
    }

    entries.sort((a, b) => a.sort_order - b.sort_order);

    batch(() => {
      setCache(date, [...entries]);
      setSaved(date, entries.map((e) => ({ ...e })));
    });
  });

  // Compute which dates have unsaved changes
  const unsavedDates = () => {
    const dates: string[] = [];
    for (const date of Object.keys(cache)) {
      if (isDirty(date)) {
        dates.push(date);
      }
    }
    dates.sort();
    return dates;
  };

  const isDirty = (date: string): boolean => {
    const current = cache[date];
    const original = saved[date];
    if (!current || !original) return !!current;
    if (current.length !== original.length) return true;
    for (let i = 0; i < current.length; i++) {
      if (
        current[i].meal_name !== original[i].meal_name ||
        current[i].content !== original[i].content
      ) {
        return true;
      }
    }
    return false;
  };

  const hasUnsaved = () => unsavedDates().length > 0;

  const currentMeals = () => cache[selectedDate()] ?? [];

  const updateMealContent = (index: number, content: string) => {
    const date = selectedDate();
    setCache(
      produce((c) => {
        if (c[date]) {
          c[date][index].content = content;
        }
      })
    );
  };

  const removeMeal = (index: number) => {
    const date = selectedDate();
    setCache(
      produce((c) => {
        if (c[date]) {
          c[date].splice(index, 1);
          // Re-index sort_order
          c[date].forEach((m, i) => (m.sort_order = i));
        }
      })
    );
  };

  const addMeal = () => {
    const name = newMealName().trim();
    if (!name) return;
    const date = selectedDate();
    const meals = cache[date] ?? [];
    if (meals.find((m) => m.meal_name === name)) return; // Already exists

    setCache(
      produce((c) => {
        if (!c[date]) c[date] = [];
        c[date].push({
          meal_name: name,
          content: "",
          sort_order: c[date].length,
        });
      })
    );
    setNewMealName("");
  };

  const saveAll = async () => {
    setSaving(true);
    const dates = unsavedDates();
    for (const date of dates) {
      const meals = cache[date];
      if (!meals) continue;
      const toSave: Meal[] = meals.map((m, i) => ({
        date,
        meal_name: m.meal_name,
        content: m.content,
        sort_order: i,
      }));
      await api.saveMeals(date, toSave);
      setSaved(date, meals.map((e) => ({ ...e })));
    }
    setSaving(false);
  };

  const goDay = (offset: number) => {
    const d = parseDate(selectedDate());
    d.setDate(d.getDate() + offset);
    setSelectedDate(formatDate(d));
  };

  const handleDateInput = (e: Event) => {
    const val = (e.target as HTMLInputElement).value;
    setDateInput(val);
    // Validate YYYY-MM-DD format
    if (/^\d{4}-\d{2}-\d{2}$/.test(val)) {
      const d = parseDate(val);
      if (!isNaN(d.getTime())) {
        setSelectedDate(val);
      }
    }
  };

  const handleExport = (format: string) => {
    window.open(api.exportUrl(format), "_blank");
  };

  return (
    <main class="tracker">
      {/* Unsaved changes banner */}
      <Show when={hasUnsaved()}>
        <div class="unsaved-banner">
          <button
            class="banner-toggle"
            onClick={() => setBannerOpen(!bannerOpen())}
          >
            <span class="banner-icon">{bannerOpen() ? "▾" : "▸"}</span>
            <span>
              {unsavedDates().length} unsaved{" "}
              {unsavedDates().length === 1 ? "day" : "days"}
            </span>
          </button>
          <Show when={bannerOpen()}>
            <div class="banner-dates">
              <For each={unsavedDates()}>
                {(date) => (
                  <button
                    class="banner-date-btn"
                    classList={{ active: date === selectedDate() }}
                    onClick={() => setSelectedDate(date)}
                  >
                    {displayDate(date)}
                  </button>
                )}
              </For>
            </div>
          </Show>
        </div>
      </Show>

      {/* Date navigation */}
      <div class="date-nav">
        <button class="btn btn-icon" onClick={() => goDay(-1)} title="Previous day">
          ←
        </button>
        <input
          type="date"
          class="date-picker"
          value={dateInput()}
          onInput={handleDateInput}
        />
        <button class="btn btn-icon" onClick={() => goDay(1)} title="Next day">
          →
        </button>
      </div>

      <h2 class="date-display">{displayDate(selectedDate())}</h2>

      {/* Meal entries */}
      <div class="meals">
        <For each={currentMeals()}>
          {(meal, index) => (
            <div class="meal-card">
              <div class="meal-header">
                <h3 class="meal-name">{meal.meal_name}</h3>
                <button
                  class="btn btn-ghost btn-sm remove-btn"
                  onClick={() => removeMeal(index())}
                  title={`Remove ${meal.meal_name}`}
                >
                  ×
                </button>
              </div>
              <textarea
                class="meal-input"
                value={meal.content}
                onInput={(e) =>
                  updateMealContent(index(), e.currentTarget.value)
                }
                maxLength={4095}
                placeholder={`What did you have for ${meal.meal_name.toLowerCase()}?`}
                rows={3}
              />
              <div class="char-count">
                {meal.content.length} / 4095
              </div>
            </div>
          )}
        </For>
      </div>

      {/* Add meal */}
      <div class="add-meal">
        <input
          type="text"
          class="add-meal-input"
          value={newMealName()}
          onInput={(e) => setNewMealName(e.currentTarget.value)}
          onKeyDown={(e) => e.key === "Enter" && addMeal()}
          placeholder="Add a meal (e.g., Second Breakfast)"
          maxLength={64}
        />
        <button
          class="btn btn-secondary"
          onClick={addMeal}
          disabled={!newMealName().trim()}
        >
          + Add
        </button>
      </div>

      {/* Action bar */}
      <div class="action-bar">
        <button
          class="btn btn-primary save-btn"
          onClick={saveAll}
          disabled={!hasUnsaved() || saving()}
        >
          {saving() ? "Saving..." : "Save changes"}
        </button>

        <div class="export-group">
          <span class="export-label">Export:</span>
          <button class="btn btn-ghost btn-sm" onClick={() => handleExport("csv")}>
            CSV
          </button>
          <button class="btn btn-ghost btn-sm" onClick={() => handleExport("xlsx")}>
            XLSX
          </button>
          <button class="btn btn-ghost btn-sm" onClick={() => handleExport("ods")}>
            ODS
          </button>
        </div>
      </div>
    </main>
  );
}
