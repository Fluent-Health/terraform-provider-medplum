# Import by Bot id. The membership is discovered automatically and source_hash
# is recomputed from the deployed code. The first plan after import shows an
# in-place update recording your code/source_path in state; a matching bundle
# is NOT re-deployed.
terraform import medplum_bot.subscription_handler 00000000-0000-0000-0000-000000000000
