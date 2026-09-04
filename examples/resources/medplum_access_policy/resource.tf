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

# A policy that constrains the VALUE a role may write, not just the interaction.
#
# `interaction` can say "you may update a Task"; it cannot say "you may move it into
# the validator-approved state but not the supervisor-approved one". That is a
# constraint on the submitted resource, which is what write_constraint expresses.
#
# ⚠️ Only the FIRST resource entry matching the type and interaction is consulted,
# and every policy on a membership is flattened into one list in assignment order.
# Do not pair this with another policy granting an unconstrained Task update —
# whichever comes first decides, and an unconstrained entry silently wins.
resource "medplum_access_policy" "document_validator" {
  name = "document-validator"

  resource {
    resource_type = "Task"
    interaction   = ["read", "search", "update"]

    write_constraint {
      description = "may only move a task into a validator-stage status"
      expression  = "%after.businessStatus.text.startsWith('workflow:validator:')"
    }

    write_constraint {
      description = "may not act on a task already escalated to a supervisor"
      expression  = "%before.businessStatus.text.startsWith('workflow:supervisor:').not()"
    }
  }
}
