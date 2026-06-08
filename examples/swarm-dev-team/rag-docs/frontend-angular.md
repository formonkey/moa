# Angular 21 — Architecture & Best Practices

## Project Structure

```
src/
├── app/
│   ├── core/           # Singleton services, guards, interceptors
│   ├── shared/         # Reusable components, pipes, directives
│   ├── features/       # Feature modules (lazy-loaded)
│   │   ├── dashboard/
│   │   ├── tasks/
│   │   └── settings/
│   ├── app.component.ts
│   ├── app.config.ts
│   └── app.routes.ts
├── assets/
├── environments/
└── styles/
```

## Key Patterns

### Standalone Components (Angular 21+)
All components MUST be standalone. No NgModules.

```typescript
@Component({
  standalone: true,
  selector: 'app-task-list',
  imports: [CommonModule, RouterModule],
  template: `...`
})
export class TaskListComponent {}
```

### State Management with Signals
Use Angular Signals for reactive state. No RxJS for simple state.

```typescript
@Injectable({ providedIn: 'root' })
export class TaskStore {
  private _tasks = signal<Task[]>([]);
  readonly tasks = this._tasks.asReadonly();
  
  addTask(task: Task) {
    this._tasks.update(current => [...current, task]);
  }
}
```

### Tailwind CSS
Use Tailwind 4 with `@apply` for reusable styles. Dark mode via `class` strategy.

## Performance Rules
- Lazy load all feature routes
- Use `changeDetection: OnPush` everywhere
- Use `trackBy` in all `@for` loops
- Defer heavy components with `@defer`
