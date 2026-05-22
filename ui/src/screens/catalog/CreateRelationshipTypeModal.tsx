import * as Dialog from "@radix-ui/react-dialog";
import { useForm } from "react-hook-form";
import { useMutation, useQueryClient } from "@tanstack/react-query";
import { z } from "zod";
import { apiClient } from "../../api/client";
import type { RelationshipType } from "../../api/resources/relationshipType";
import { useAppStore } from "../../state/store";
import styles from "./CreateRelationshipTypeModal.module.css";

const schema = z.object({
  name: z
    .string()
    .regex(/^[a-z0-9]([a-z0-9-]*[a-z0-9])?$/, "lowercase alphanumeric / hyphen")
    .max(253),
  displayName: z.string().optional(),
  description: z.string().optional(),
  subjectGVK: z.object({
    group: z.string(),
    version: z.string().min(1, "required"),
    kind: z.string().min(1, "required"),
  }),
  objectGVK: z.object({
    group: z.string(),
    version: z.string().min(1, "required"),
    kind: z.string().min(1, "required"),
  }),
  cardinality: z.enum(["OneToOne", "OneToMany", "ManyToMany"]),
});

type FormValues = z.infer<typeof schema>;

interface Props {
  open: boolean;
  onClose: () => void;
}

const DEFAULT_VALUES: FormValues = {
  name: "",
  displayName: "",
  description: "",
  subjectGVK: { group: "", version: "v1", kind: "" },
  objectGVK: { group: "", version: "v1", kind: "" },
  cardinality: "OneToMany",
};

export default function CreateRelationshipTypeModal({ open, onClose }: Props) {
  const qc = useQueryClient();
  const addToast = useAppStore((s) => s.addToast);
  const {
    register,
    handleSubmit,
    reset,
    setError,
    formState: { errors, isSubmitting },
  } = useForm<FormValues>({ defaultValues: DEFAULT_VALUES });

  const mutation = useMutation({
    mutationFn: (rt: RelationshipType) =>
      apiClient.post<RelationshipType>(
        "/apis/knowledge.miloapis.com/v1alpha1/relationshiptypes",
        rt,
      ),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: ["knowledge", "v1alpha1", "relationshiptypes"],
      });
      addToast({ kind: "success", title: "RelationshipType created" });
      reset(DEFAULT_VALUES);
      onClose();
    },
    onError: (err) => {
      addToast({
        kind: "error",
        title: "Failed to create RelationshipType",
        description: err instanceof Error ? err.message : String(err),
      });
    },
  });

  const onSubmit = (values: FormValues) => {
    const parsed = schema.safeParse(values);
    if (!parsed.success) {
      for (const issue of parsed.error.issues) {
        const path = issue.path.join(".") as keyof FormValues;
        setError(path as never, { message: issue.message });
      }
      return;
    }
    const v = parsed.data;
    const obj: RelationshipType = {
      apiVersion: "knowledge.miloapis.com/v1alpha1",
      kind: "RelationshipType",
      metadata: { name: v.name },
      spec: {
        displayName: v.displayName || undefined,
        description: v.description || undefined,
        subjectGVK: v.subjectGVK,
        objectGVK: v.objectGVK,
        cardinality: v.cardinality,
      },
    };
    return mutation.mutateAsync(obj);
  };

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(o) => {
        if (!o) {
          reset(DEFAULT_VALUES);
          onClose();
        }
      }}
    >
      <Dialog.Portal>
        <Dialog.Overlay className={styles.overlay} />
        <Dialog.Content className={styles.content}>
          <Dialog.Title className={styles.title}>
            Create Relationship Type
          </Dialog.Title>
          <Dialog.Description className={styles.subtitle}>
            Declare a new typed edge schema.
          </Dialog.Description>

          <form onSubmit={handleSubmit(onSubmit)} className={styles.form}>
            <div className={styles.field}>
              <label className={styles.label}>Name</label>
              <input
                {...register("name")}
                className={styles.input}
                placeholder="domain-attached-to-gateway"
                autoFocus
              />
              {errors.name && (
                <div className={styles.error}>{errors.name.message}</div>
              )}
            </div>

            <div className={styles.field}>
              <label className={styles.label}>Display name</label>
              <input
                {...register("displayName")}
                className={styles.input}
                placeholder="Attached To"
              />
            </div>

            <div className={styles.field}>
              <label className={styles.label}>Description</label>
              <textarea
                {...register("description")}
                className={styles.textarea}
                rows={2}
              />
            </div>

            <fieldset className={styles.group}>
              <legend className={styles.legend}>Subject GVK</legend>
              <div className={styles.gvkRow}>
                <input
                  {...register("subjectGVK.group")}
                  className={styles.input}
                  placeholder="group"
                />
                <input
                  {...register("subjectGVK.version")}
                  className={styles.input}
                  placeholder="version"
                />
                <input
                  {...register("subjectGVK.kind")}
                  className={styles.input}
                  placeholder="kind"
                />
              </div>
              {(errors.subjectGVK?.version || errors.subjectGVK?.kind) && (
                <div className={styles.error}>
                  {errors.subjectGVK?.version?.message ||
                    errors.subjectGVK?.kind?.message}
                </div>
              )}
            </fieldset>

            <fieldset className={styles.group}>
              <legend className={styles.legend}>Object GVK</legend>
              <div className={styles.gvkRow}>
                <input
                  {...register("objectGVK.group")}
                  className={styles.input}
                  placeholder="group"
                />
                <input
                  {...register("objectGVK.version")}
                  className={styles.input}
                  placeholder="version"
                />
                <input
                  {...register("objectGVK.kind")}
                  className={styles.input}
                  placeholder="kind"
                />
              </div>
              {(errors.objectGVK?.version || errors.objectGVK?.kind) && (
                <div className={styles.error}>
                  {errors.objectGVK?.version?.message ||
                    errors.objectGVK?.kind?.message}
                </div>
              )}
            </fieldset>

            <div className={styles.field}>
              <label className={styles.label}>Cardinality</label>
              <select {...register("cardinality")} className={styles.input}>
                <option value="OneToOne">OneToOne</option>
                <option value="OneToMany">OneToMany</option>
                <option value="ManyToMany">ManyToMany</option>
              </select>
            </div>

            <div className={styles.actions}>
              <Dialog.Close asChild>
                <button type="button" className={styles.secondaryBtn}>
                  Cancel
                </button>
              </Dialog.Close>
              <button
                type="submit"
                className={styles.primaryBtn}
                disabled={isSubmitting || mutation.isPending}
              >
                {mutation.isPending ? "Creating…" : "Create"}
              </button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
