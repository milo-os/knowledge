import { useEffect, useMemo } from "react";
import { useSearchParams } from "react-router-dom";
import { useForm, Controller, type Resolver } from "react-hook-form";
import { z } from "zod";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import * as Checkbox from "@radix-ui/react-checkbox";
import * as RadioGroup from "@radix-ui/react-radio-group";
import * as Slider from "@radix-ui/react-slider";

import styles from "./QueryBuilderModal.module.css";
import { apiClient } from "../../api/client";
import type { KubernetesList } from "../../api/client";
import type {
  GraphQuery,
  GraphQueryResult,
  GraphQuerySpec,
} from "../../api/resources/graphQuery";
import type { RelationshipType } from "../../api/resources/relationshipType";
import { useAppStore } from "../../state/store";

const schema = z.object({
  root: z.object({
    apiGroup: z.string().min(1),
    kind: z.string().min(1),
    name: z.string().min(1),
    namespace: z.string().optional(),
    controlPlaneContextRef: z.object({
      kind: z.enum(["Platform", "Organization", "Project", "User"]),
      name: z.string().min(1),
    }),
  }),
  traverse: z.object({
    relationshipTypes: z.array(z.string()).optional(),
    direction: z.enum(["Outbound", "Inbound", "Both"]).default("Both"),
    maxDepth: z.number().int().min(1).max(10).default(3),
    maxNodes: z.number().int().min(50).max(1000).default(500),
  }),
});

type FormValues = z.infer<typeof schema>;

const zodResolver =
  <T extends z.ZodTypeAny>(s: T): Resolver<z.infer<T>> =>
  async (values) => {
    const r = s.safeParse(values);
    if (r.success) return { values: r.data, errors: {} };
    const errors: Record<string, unknown> = {};
    for (const issue of r.error.issues) {
      let cursor = errors;
      for (let i = 0; i < issue.path.length - 1; i++) {
        const key = String(issue.path[i]);
        if (typeof cursor[key] !== "object" || cursor[key] === null) {
          cursor[key] = {};
        }
        cursor = cursor[key] as Record<string, unknown>;
      }
      const last = String(issue.path[issue.path.length - 1]);
      cursor[last] = { type: issue.code, message: issue.message };
    }
    return { values: {}, errors: errors as never };
  };

function defaults(spec: GraphQuerySpec | null): FormValues {
  return {
    root: {
      apiGroup: spec?.root.apiGroup ?? "",
      kind: spec?.root.kind ?? "",
      name: spec?.root.name ?? "",
      namespace: spec?.root.namespace ?? "",
      controlPlaneContextRef: {
        kind: spec?.root.controlPlaneContextRef.kind ?? "Organization",
        name: spec?.root.controlPlaneContextRef.name ?? "",
      },
    },
    traverse: {
      relationshipTypes: spec?.traverse.relationshipTypes ?? [],
      direction: spec?.traverse.direction ?? "Both",
      maxDepth: spec?.traverse.maxDepth ?? 3,
      maxNodes: spec?.traverse.maxNodes ?? 500,
    },
  };
}

function specFromForm(values: FormValues): GraphQuerySpec {
  return {
    root: {
      apiGroup: values.root.apiGroup,
      kind: values.root.kind,
      name: values.root.name,
      namespace: values.root.namespace || undefined,
      controlPlaneContextRef: values.root.controlPlaneContextRef,
    },
    traverse: {
      relationshipTypes:
        values.traverse.relationshipTypes &&
        values.traverse.relationshipTypes.length > 0
          ? values.traverse.relationshipTypes
          : undefined,
      direction: values.traverse.direction,
      maxDepth: values.traverse.maxDepth,
      maxNodes: values.traverse.maxNodes,
    },
  };
}

export default function QueryBuilderModal() {
  const [searchParams, setSearchParams] = useSearchParams();
  const open = searchParams.get("query") === "open";

  const lastQuerySpec = useAppStore((s) => s.lastQuerySpec);
  const setLastQuerySpec = useAppStore((s) => s.setLastQuerySpec);
  const addToast = useAppStore((s) => s.addToast);
  const queryClient = useQueryClient();

  const close = () => {
    const next = new URLSearchParams(searchParams);
    next.delete("query");
    setSearchParams(next, { replace: true });
  };

  const lastResult = lastQuerySpec
    ? queryClient.getQueryData<GraphQueryResult>([
        "graphquery",
        JSON.stringify(lastQuerySpec),
      ])
    : undefined;
  const showTruncationBanner = lastResult?.status.truncated === true;

  const { control, register, handleSubmit, reset, formState } =
    useForm<FormValues>({
      resolver: zodResolver(schema),
      defaultValues: defaults(lastQuerySpec),
    });

  useEffect(() => {
    if (open) reset(defaults(lastQuerySpec));
  }, [open, lastQuerySpec, reset]);

  const typesQuery = useQuery({
    queryKey: ["relationshiptypes"],
    queryFn: () =>
      apiClient.get<KubernetesList<RelationshipType>>(
        "/apis/knowledge.miloapis.com/v1alpha1/relationshiptypes",
      ),
    enabled: open,
  });

  const mutation = useMutation({
    mutationFn: async (spec: GraphQuerySpec) => {
      const ns = spec.root.namespace || "default";
      const body: GraphQuery = {
        apiVersion: "knowledge.miloapis.com/v1alpha1",
        kind: "GraphQuery",
        spec,
      };
      const created = await apiClient.post<GraphQuery>(
        `/apis/knowledge.miloapis.com/v1alpha1/namespaces/${encodeURIComponent(ns)}/graphqueries`,
        body,
      );
      const result: GraphQueryResult = {
        nodes: created.status?.nodes ?? [],
        edges: created.status?.edges ?? [],
        status: { truncated: created.status?.truncated ?? false },
      };
      return { spec, result };
    },
    onSuccess: ({ spec, result }) => {
      queryClient.setQueryData(["graphquery", JSON.stringify(spec)], result);
      setLastQuerySpec(spec);
      close();
    },
    onError: (err) => {
      addToast({
        kind: "error",
        title: "Query failed",
        description: err instanceof Error ? err.message : String(err),
      });
    },
  });

  const onSubmit = handleSubmit((values) => {
    mutation.mutate(specFromForm(values));
  });

  const relTypes = useMemo(
    () => typesQuery.data?.items ?? [],
    [typesQuery.data],
  );

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(o) => {
        if (!o) close();
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className={styles.overlay} />
        <Dialog.Content className={styles.content}>
          <Dialog.Title className={styles.title}>Build Query</Dialog.Title>
          <Dialog.Description className={styles.sectionLabel}>
            Traverse the knowledge graph from a root resource.
          </Dialog.Description>

          {showTruncationBanner && (
            <div className={styles.banner}>
              Previous query was truncated — try reducing maxNodes or maxDepth.
            </div>
          )}

          <form onSubmit={onSubmit}>
            <div className={styles.section}>
              <div className={styles.sectionLabel}>Root resource</div>
              <div className={styles.grid}>
                <div className={styles.field}>
                  <label htmlFor="apiGroup">API group</label>
                  <input
                    id="apiGroup"
                    className={styles.input}
                    {...register("root.apiGroup")}
                  />
                  {formState.errors.root?.apiGroup && (
                    <span className={styles.fieldError}>
                      {formState.errors.root.apiGroup.message}
                    </span>
                  )}
                </div>
                <div className={styles.field}>
                  <label htmlFor="kind">Kind</label>
                  <input
                    id="kind"
                    className={styles.input}
                    {...register("root.kind")}
                  />
                  {formState.errors.root?.kind && (
                    <span className={styles.fieldError}>
                      {formState.errors.root.kind.message}
                    </span>
                  )}
                </div>
                <div className={styles.field}>
                  <label htmlFor="name">Name</label>
                  <input
                    id="name"
                    className={styles.input}
                    {...register("root.name")}
                  />
                  {formState.errors.root?.name && (
                    <span className={styles.fieldError}>
                      {formState.errors.root.name.message}
                    </span>
                  )}
                </div>
                <div className={styles.field}>
                  <label htmlFor="namespace">Namespace</label>
                  <input
                    id="namespace"
                    className={styles.input}
                    {...register("root.namespace")}
                  />
                </div>
                <div className={styles.field}>
                  <label htmlFor="ctxKind">Context kind</label>
                  <select
                    id="ctxKind"
                    className={styles.select}
                    {...register("root.controlPlaneContextRef.kind")}
                  >
                    <option value="Platform">Platform</option>
                    <option value="Organization">Organization</option>
                    <option value="Project">Project</option>
                    <option value="User">User</option>
                  </select>
                </div>
                <div className={styles.field}>
                  <label htmlFor="ctxName">Context name</label>
                  <input
                    id="ctxName"
                    className={styles.input}
                    {...register("root.controlPlaneContextRef.name")}
                  />
                </div>
              </div>
            </div>

            <div className={styles.section} style={{ marginTop: 16 }}>
              <div className={styles.sectionLabel}>Relationship types</div>
              <Controller
                control={control}
                name="traverse.relationshipTypes"
                render={({ field }) => {
                  const selected = field.value ?? [];
                  const toggle = (name: string, checked: boolean) => {
                    if (checked) {
                      field.onChange([...selected, name]);
                    } else {
                      field.onChange(selected.filter((n) => n !== name));
                    }
                  };
                  return (
                    <div className={styles.checkboxList}>
                      {typesQuery.isLoading && (
                        <span className={styles.sectionLabel}>Loading…</span>
                      )}
                      {!typesQuery.isLoading && relTypes.length === 0 && (
                        <span className={styles.sectionLabel}>
                          No relationship types registered.
                        </span>
                      )}
                      {relTypes.map((rt) => {
                        const name = rt.metadata.name;
                        const checked = selected.includes(name);
                        return (
                          <label key={name} className={styles.checkboxRow}>
                            <Checkbox.Root
                              className={styles.checkbox}
                              checked={checked}
                              onCheckedChange={(c) =>
                                toggle(name, c === true)
                              }
                            >
                              <Checkbox.Indicator
                                className={styles.checkboxIndicator}
                              >
                                ✓
                              </Checkbox.Indicator>
                            </Checkbox.Root>
                            <span>{rt.spec.displayName ?? name}</span>
                          </label>
                        );
                      })}
                    </div>
                  );
                }}
              />
            </div>

            <div className={styles.section} style={{ marginTop: 16 }}>
              <div className={styles.sectionLabel}>Direction</div>
              <Controller
                control={control}
                name="traverse.direction"
                render={({ field }) => (
                  <RadioGroup.Root
                    className={styles.radioGroup}
                    value={field.value}
                    onValueChange={field.onChange}
                  >
                    {(["Outbound", "Inbound", "Both"] as const).map((d) => (
                      <label key={d} className={styles.radioRow}>
                        <RadioGroup.Item value={d} className={styles.radio}>
                          <RadioGroup.Indicator
                            className={styles.radioIndicator}
                          />
                        </RadioGroup.Item>
                        <span>{d}</span>
                      </label>
                    ))}
                  </RadioGroup.Root>
                )}
              />
            </div>

            <div className={styles.section} style={{ marginTop: 16 }}>
              <div className={styles.sectionLabel}>Max depth</div>
              <Controller
                control={control}
                name="traverse.maxDepth"
                render={({ field }) => (
                  <div className={styles.sliderRow}>
                    <Slider.Root
                      className={styles.sliderRoot}
                      min={1}
                      max={10}
                      step={1}
                      value={[field.value]}
                      onValueChange={(v) => field.onChange(v[0])}
                    >
                      <Slider.Track className={styles.sliderTrack}>
                        <Slider.Range className={styles.sliderRange} />
                      </Slider.Track>
                      <Slider.Thumb className={styles.sliderThumb} />
                    </Slider.Root>
                    <span className={styles.sliderValue}>{field.value}</span>
                  </div>
                )}
              />
            </div>

            <div className={styles.section} style={{ marginTop: 16 }}>
              <div className={styles.sectionLabel}>Max nodes</div>
              <Controller
                control={control}
                name="traverse.maxNodes"
                render={({ field }) => (
                  <div className={styles.sliderRow}>
                    <Slider.Root
                      className={styles.sliderRoot}
                      min={50}
                      max={1000}
                      step={50}
                      value={[field.value]}
                      onValueChange={(v) => field.onChange(v[0])}
                    >
                      <Slider.Track className={styles.sliderTrack}>
                        <Slider.Range className={styles.sliderRange} />
                      </Slider.Track>
                      <Slider.Thumb className={styles.sliderThumb} />
                    </Slider.Root>
                    <span className={styles.sliderValue}>{field.value}</span>
                  </div>
                )}
              />
            </div>

            <div className={styles.footer}>
              <button
                type="button"
                className={styles.btn}
                onClick={close}
                disabled={mutation.isPending}
              >
                Cancel
              </button>
              <button
                type="submit"
                className={`${styles.btn} ${styles.btnPrimary}`}
                disabled={mutation.isPending}
              >
                {mutation.isPending ? "Running…" : "Run Query"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
