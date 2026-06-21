#!/bin/bash
# FM Phase 2.1: Documentation File Rename Commands
# Run this script after verifying content updates are correct
# Usage: bash PHASE_2_1_RENAME_COMMANDS.sh

echo "FM Phase 2.1: Renaming 8 design files..."
echo ""

# CM (Config Management) - 3 files
echo "Renaming CM (Config Management) files..."
git mv docs/FM-Designs/FM_DESIGN_LAYER1_CONFIG_PLANE.md docs/FM-Designs/FM_DESIGN_CM_CONFIG_MANAGEMENT.md
git mv docs/FM-Designs/FM_DESIGN_LAYER1_CONFIG_PLANE_ENHANCED.md docs/FM-Designs/FM_DESIGN_CM_CONFIG_MANAGEMENT_ENHANCED.md
git mv docs/FM-Designs/FM_DESIGN_LAYER1_CONFIG_PLANE_SUPER_ENHANCED.md docs/FM-Designs/FM_DESIGN_CM_CONFIG_MANAGEMENT_SUPER_ENHANCED.md

# DM (Data Management) - 3 files
echo "Renaming DM (Data Management) files..."
git mv docs/FM-Designs/FM_DESIGN_LAYER2_DATABASE_MODEL.md docs/FM-Designs/FM_DESIGN_DM_DATA_MANAGEMENT.md
git mv docs/FM-Designs/FM_DESIGN_LAYER2_DATABASE_MODEL_ENHANCED.md docs/FM-Designs/FM_DESIGN_DM_DATA_MANAGEMENT_ENHANCED.md
git mv docs/FM-Designs/FM_DESIGN_LAYER2_DATABASE_MODEL_SUPER_ENHANCED.md docs/FM-Designs/FM_DESIGN_DM_DATA_MANAGEMENT_SUPER_ENHANCED.md

# GM (Goal State Management) - 2 files
echo "Renaming GM (Goal State Management) files..."
git mv docs/FM-Designs/FM_DESIGN_LAYER3_SOUTHBOUND.md docs/FM-Designs/FM_DESIGN_GM_GOAL_STATE_MANAGEMENT.md
git mv docs/FM-Designs/FM_DESIGN_LAYER3_SOUTHBOUND_SUPER_ENHANCED.md docs/FM-Designs/FM_DESIGN_GM_GOAL_STATE_MANAGEMENT_SUPER_ENHANCED.md

# DAL (DPU Abstraction Layer) - 2 files
echo "Renaming DAL (DPU Abstraction Layer) files..."
git mv docs/FM-Designs/FM_DESIGN_LAYER4_PLUGIN.md docs/FM-Designs/FM_DESIGN_DAL_DPU_ABSTRACTION.md
git mv docs/FM-Designs/FM_DESIGN_LAYER4_PLUGIN_SUPER_ENHANCED.md docs/FM-Designs/FM_DESIGN_DAL_DPU_ABSTRACTION_SUPER_ENHANCED.md

echo ""
echo "✓ All 8 files renamed successfully!"
echo ""
echo "Next steps:"
echo "1. Review the renamed files: git status"
echo "2. Stage changes: git add ."
echo "3. Commit: git commit -m 'docs: rename design files from Layer 1/2/3/4 to CM/DM/GM/DAL'"
