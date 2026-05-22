import { useEffect, useRef, useState } from "react";
import { useForm } from "react-hook-form";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useNavigate } from "react-router-dom";
import { z } from "zod";
import { apiClient, type KubernetesList } from "../../api/client";
import type { RelationshipType } from "../../api/resources/relationshipType";
import type { RelationshipPolicy } from "../../api/resources/relationshipPolicy";
import { useAppStore } from "../../state/store";
import styles from "./PolicyCreatePage.module.css";

const schema = z.object({
  name: z
    .string()
    .regex(/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/, "lowercase alphanumeric / hyphen")
    .max(253),
  namespace: z
    .string()
    .regex(/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/)
    .max(253),
  relationshipType: z.string().min(1, "select a type"),
  subjectKind: z.string().min(1, "required"),
  expression: z.string().min(1, "required"),
});

type FormValues = z.infer<typeof schema>;

const POLL_INTERVAL = 2000;
const POLL_TIMEOUT = 30_000;

export default function PolicyCreatePage() {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const addToast = useAppStore((s) => s.addToast);
  const ctx = useAppStore((s) => s.currentContext);
  const [polling, setPolling] = useState<{ ns: string; name: string } | null>(null);
  const [statusError, setStatusError] = useState<string | null>(null);
  const pollStart = useRef<number>(0);

  const {
    register,
    handleSubmit,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({
    defaultValues: {
      name: "",
      namespace: ctx?.name ?? "default",
      relationshipType: "",
      subjectKind: "",
      expression: "",
    },
  });

  const typesQuery = useQuery({
    queryKey: ["knowledge", "v1alpha1", "relationshiptypes"],
    queryFn: () =>
      apiClient.get<KubernetesList<RelationshipType>>(
        "/apis/knowledge.miloapis.com/v1alpha1/relationshiptypes",
      ),
  });

  const pollQuery = useQuery({
    queryKey: ["knowledge", "policy", polling?.ns, polling?.name],
    enabled: !!polling,
    refetchInterval: polling ? POLL_INTERVAL : false,
    queryFn: () =>
      apiClient.get<RelationshipPolicy>(
        `/apis/knowledge.miloapis.com/v1alpha1/namespaces/${polling!.ns}/relationshippolicies/${polling!.name}`,
      ),
  });

  useEffect(() => {
    if (!polling || !pollQuery.data) return;
    const conds = pollQuery.data.status?.conditions ?? [];
    const failure = conds.find(
      (c) =>
        c.status === "False" &&
        (c.type === "ExpressionError" || c.type === "EvalError"),
    );
    if (failure) {
      setStatusError(failure.message ?? `${failure.type}: ${failure.reason ?? "failed"}`);
      setPolling(null);
      return;
    }
    if (conds.length > 0 && conds.every((c) => c.status === "True")) {
      addToast({ kind: "success", title: "RelationshipPolicy created" });
      setPolling(null);
      navigate("/catalog#policies");
      return;
    }
    if (Date.now() - pollStart.current > POLL_TIMEOUT) {
      setStatusError(
        "Policy created but its status did not become Ready within 30s.",
      );
      setPolling(null);
    }
  }, [polling, pollQuery.data, addToast, navigate]);

  const createMutation = useMutation({
    mutationFn: (policy: RelationshipPolicy) =>
      apiClient.post<RelationshipPolicy>(
        `/apis/knowledge.miloapis.com/v1alpha1/namespaces/${policy.metadata.namespace}/relationshippolicies`,
        policy,
      ),
    onSuccess: (created) => {
      qc.invalidateQueries({
        queryKey: ["knowledge", "v1alpha1", "relationshippolicies", created.metadata.namespace],
      });
      pollStart.current = Date.now();
      setStatusError(null);
      setPolling({
        ns: created.metadata.namespace!,
        name: created.metadata.name,
      });
    },
    onError: (err) => {
      setStatusError(err instanceof Error ? err.message : String(err));
    },
  });

  const onSubmit = (values: FormValues) => {
    const parsed = schema.safeParse(values);
    if (!parsed.success) {
      for (const issue of parsed.error.issues) {
        setError(issue.path.join(".") as keyof FormValues, {
          message: issue.message,
        });
      }
      return;
    }
    const v = parsed.data;
    const selected = typesQuery.data?.items.find(
      (t) => t.metadata.name === v.relationshipType,
    );
    const subjectGroup = selected?.spec.subjectGVK.group ?? "";

    const policy: RelationshipPolicy = {
      apiVersion: "knowledge.miloapis.com/v1alpha1",
      kind: "RelationshipPolicy",
      metadata: { name: v.name, namespace: v.namespace },
      spec: {
        relationshipType: { name: v.relationshipType },
        subject: { apiGroup: subjectGroup, kind: v.subjectKind },
        expression: v.expression,
        controlPlaneContextRef: ctx ?? { kind: "Platform", name: v.namespace },
      },
    };
    return createMutation.mutateAsync(policy);
  };

  const types = typesQuery.data?.items ?? [];
  const submitting = isSubmitting || createMutation.isPending || !!polling;

  return (
    <div className={styles.page}>
      <header className={styles.header}>
        <h1 className={styles.title}>Create Discovery Policy</h1>
        <button
          type="button"
          className={styles.secondaryBtn}
          onClick={() => navigate("/catalog#policies")}
        >
          Cancel
        </button>
      </header>

      <form onSubmit={handleSubmit(onSubmit)} className={styles.form}>
        {statusError && <div className={styles.alert}>{statusError}</div>}

        <div className={styles.row}>
          <div className={styles.field}>
            <label className={styles.label}>Name</label>
            <input
              {...register("name")}
              className={styles.input}
              placeholder="domain-attached-policy"
              autoFocus
            />
            {errors.name && <div className={styles.error}>{errors.name.message}</div>}
          </div>

          <div className={styles.field}>
            <label className={styles.label}>Namespace</label>
            <input {...register("namespace")} className={styles.input} />
            {errors.namespace && (
              <div className={styles.error}>{errors.namespace.message}</div>
            )}
          </div>
        </div>

        <div className={styles.field}>
          <label className={styles.label}>Relationship Type</label>
          <select {...register("relationshipType")} className={styles.input}>
            <option value="">— select —</option>
            {types.map((t) => (
              <option key={t.metadata.name} value={t.metadata.name}>
                {t.metadata.name}
              </option>
            ))}
          </select>
          {errors.relationshipType && (
            <div className={styles.error}>{errors.relationshipType.message}</div>
          )}
        </div>

        <div className={styles.field}>
          <label className={styles.label}>Subject Kind</label>
          <input
            {...register("subjectKind")}
            className={styles.input}
            placeholder="Domain"
          />
          {errors.subjectKind && (
            <div className={styles.error}>{errors.subjectKind.message}</div>
          )}
        </div>

        <div className={styles.field}>
          <label className={styles.label}>CEL Expression</label>
          <textarea
            {...register("expression")}
            className={styles.textarea}
            rows={8}
            placeholder={`[{
  "apiGroup": "networking.miloapis.com",
  "kind": "Gateway",
  "name": subject.spec.gatewayName
}]`}
          />
          {errors.expression && (
            <div className={styles.error}>{errors.expression.message}</div>
          )}
        </div>

        <div className={styles.actions}>
          <button
            type="submit"
            className={styles.primaryBtn}
            disabled={submitting}
          >
            {polling
              ? "Waiting for status…"
              : createMutation.isPending
                ? "Creating…"
                : "Create Policy"}
          </button>
        </div>
      </form>
    </div>
  );
}
