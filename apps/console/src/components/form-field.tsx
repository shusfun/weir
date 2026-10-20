import { Input, Label, Textarea } from "@migate/ui";
export function Field({
  label,
  name,
  type = "text",
  value,
  onChange,
  required = true,
  autoComplete,
}: {
  label: string;
  name: string;
  type?: string;
  value: any;
  onChange: (v: string) => void;
  required?: boolean;
  autoComplete?: string;
}) {
  return (
    <div className="field">
      <Label htmlFor={name}>{label}</Label>
      {type === "textarea" ? (
        <Textarea
          id={name}
          value={value ?? ""}
          rows={5}
          required={required}
          onChange={(e) => onChange(e.target.value)}
          autoComplete="off"
        />
      ) : (
        <Input
          id={name}
          type={type}
          value={value ?? ""}
          onChange={(e) => onChange(e.target.value)}
          required={required}
          autoComplete={autoComplete ?? (type === "password" ? "new-password" : undefined)}
        />
      )}
    </div>
  );
}
