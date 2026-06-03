# An access policy that allows read-only access to Patient and Observation
# resources, with a top-level patient compartment filter.
resource "medplum_access_policy" "read_only_clinical" {
  name        = "read-only-clinical"
  compartment = "Patient/%patient.id"

  resource {
    resource_type = "Patient"
    interaction   = ["read", "search"]
  }

  resource {
    resource_type = "Observation"
    interaction   = ["read", "search"]
  }

  resource {
    resource_type = "Condition"
    interaction   = ["read", "search"]
    hidden_fields = ["note"]
  }
}

# A policy that grants full access within the project but restricts some fields.
resource "medplum_access_policy" "practitioner_full" {
  name = "practitioner-full"

  resource {
    resource_type   = "Patient"
    readonly_fields = ["meta"]
    interaction     = ["read", "write", "search", "create", "delete"]
  }

  resource {
    resource_type = "Appointment"
    interaction   = ["read", "write", "search", "create", "delete"]
  }

  ip_access_rule {
    name   = "office-network"
    value  = "203.0.113.0/24"
    action = "allow"
  }
}
